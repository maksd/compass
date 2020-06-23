package edp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/kyma-incubator/compass/components/metris/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"k8s.io/client-go/util/workqueue"
)

var (
	ErrEventInvalidRequest    = errors.New("invalid request")
	ErrEventMissingParameters = errors.New("namespace, dataStream or dataTenant not found")
	ErrEventUnknown           = errors.New("unknown error")
	ErrEventMarshal           = errors.New("marshal error")
	ErrEventHTTPRequest       = errors.New("HTTP request error")

	rateLimiterBaseDelay      = 5 * time.Second
	rateLimiterMaxDelay       = 60 * time.Second
	clientReqTimeout          = 30 * time.Second
	clientIdleConnTimeout     = 60 * time.Second
	clientTLSHandshakeTimeout = 10 * time.Second

	defaultHTTPClient = &http.Client{
		Timeout: clientReqTimeout,
		Transport: &http.Transport{
			IdleConnTimeout:     clientIdleConnTimeout,
			TLSHandshakeTimeout: clientTLSHandshakeTimeout,
		},
	}
)

// NewClient constructs a EDP client from the provided config.
func NewClient(c *Config, httpClient *http.Client, eventsChannel <-chan *Event, logger *zap.SugaredLogger) *Client {
	if httpClient == nil {
		httpClient = defaultHTTPClient
		httpClient.Timeout = c.Timeout
	}

	// retry after baseDelay*2^<num-failures>
	ratelimiter := workqueue.NewItemExponentialFailureRateLimiter(rateLimiterBaseDelay, rateLimiterMaxDelay)

	return &Client{
		config:        c,
		httpClient:    httpClient,
		queue:         workqueue.NewNamedRateLimitingQueue(ratelimiter, "edp-events"),
		logger:        logger,
		eventsChannel: eventsChannel,
	}
}

// Run starts worker process in their own goroutine, which then wait for event to process from the queue.
// Run also starts a goroutine that listen for new event to add to the queue.
// Run blocks until all workers are done or context has been canceled.
func (c *Client) Run(parentCtx context.Context, parentwg *sync.WaitGroup) {
	c.logger.Debug("starting ingester")

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	parentwg.Add(1)
	defer parentwg.Done()

	var wg sync.WaitGroup

	wg.Add(c.config.Workers)

	for i := 0; i < c.config.Workers; i++ {
		go func(i int) {
			defer wg.Done()

			workerlogger := c.logger.With("worker", i)

			for {
				event, quit := c.queue.Get()
				if quit {
					workerlogger.Debugf("shutting down")
					return
				}

				c.handleErr(
					c.Write(ctx, event.(*Event), workerlogger),
					event.(*Event),
					workerlogger,
				)

				c.queue.Done(event)
			}
		}(i)
	}

	go func() {
		for {
			select {
			case event := <-c.eventsChannel:
				c.queue.Add(event)
			case <-ctx.Done():
				c.queue.ShutDown()
				return
			}
		}
	}()

	wg.Wait()
	c.logger.Debug("stopping ingester")
}

// handleErr checks if an error happened and requeue the event.
func (c *Client) handleErr(err error, event *Event, logger *zap.SugaredLogger) {
	if err == nil {
		// if no error, clear number of queue history
		c.queue.Forget(event)

		return
	}

	// if the error is an unmarshall one, we remove it from the queue
	if errors.Is(err, ErrEventMarshal) {
		logger.Error(err)

		return
	}

	failures := c.queue.NumRequeues(event)

	// retries X times, then stops trying
	if failures < c.config.EventRetry {
		// RateLimiter `When` method increase the failure count, so it can't be used
		nextsend := when(failures)

		logger.With("error", err, "event", fmt.Sprintf("%+v", event.Data)).Errorf("error sending event, requeuing in %s (%d/%d)", nextsend, failures, c.config.EventRetry)

		// Re-enqueue the event
		c.queue.AddRateLimited(event)

		return
	}

	logger.With("error", err, "event", fmt.Sprintf("%+v", event.Data)).Errorf("failed %d times to send the event, removing it out of the queue", c.config.EventRetry)
	c.queue.Forget(event)
}

// Write sends events(json) to EDP server.
func (c *Client) Write(parentctx context.Context, event *Event, logger *zap.SugaredLogger) error {
	metricTimer := prometheus.NewTimer(metrics.SentSamplesDuration)
	defer metricTimer.ObserveDuration()

	receivedEvents := 1
	datatenant := event.Datatenant

	eventdata, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrEventMarshal, err)
	}

	edpurl := fmt.Sprintf("%s/namespaces/%s/dataStreams/%s/%s/dataTenants/%s/%s/events",
		c.config.URL,
		c.config.Namespace,
		c.config.DataStream,
		c.config.DataStreamVersion,
		datatenant,
		c.config.DataStreamEnv,
	)

	logger.Debugf("sending events '%s':\n%+v", edpurl, string(eventdata))

	httpreq, err := http.NewRequestWithContext(parentctx, "POST", edpurl, bytes.NewBuffer(eventdata))
	if err != nil {
		metrics.FailedSamples.Add(float64(receivedEvents))
		return fmt.Errorf("%w: %s", ErrEventHTTPRequest, err)
	}

	httpreq.Header.Set("User-Agent", "metris")
	httpreq.Header.Add("Content-Type", "application/json;charset=utf-8")
	httpreq.Header.Add("Authorization", "bearer "+c.config.Token)

	resp, err := c.httpClient.Do(httpreq)
	if err != nil {
		metrics.FailedSamples.Add(float64(receivedEvents))
		return fmt.Errorf("%w: %s", ErrEventHTTPRequest, err)
	}

	defer func() {
		err := resp.Body.Close()
		if err != nil {
			c.logger.Warn(err)
		}
	}()

	if resp.StatusCode != http.StatusCreated {
		metrics.FailedSamples.Add(float64(receivedEvents))
		return statusError(resp.StatusCode)
	}

	metrics.SentSamples.Add(float64(receivedEvents))

	return nil
}

// statusError converts http response code to an EDP error type.
func statusError(code int) error {
	var err error

	switch code {
	case http.StatusBadRequest:
		err = ErrEventInvalidRequest
	case http.StatusNotFound:
		err = ErrEventMissingParameters
	default:
		err = ErrEventUnknown
	}

	return fmt.Errorf("%w: %d", err, code)
}

// when returns the duration to wait before requeing event.
func when(failure int) time.Duration {
	var rateLimiterBase float64 = 2

	backoff := float64(rateLimiterBaseDelay.Nanoseconds()) * math.Pow(rateLimiterBase, float64(failure))
	if backoff > math.MaxInt64 {
		return rateLimiterMaxDelay
	}

	when := time.Duration(backoff)
	if when > rateLimiterMaxDelay {
		return rateLimiterMaxDelay
	}

	return when
}
