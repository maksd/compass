package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kyma-incubator/compass/components/metris/internal/edp"
	"github.com/kyma-incubator/compass/components/metris/internal/metrics"
	"github.com/kyma-incubator/compass/components/metris/internal/provider"
	"github.com/kyma-incubator/compass/components/metris/internal/storage"
	"github.com/kyma-incubator/compass/components/metris/internal/utils"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"k8s.io/client-go/util/workqueue"
)

var (
	// register the azure provider
	_ = func() struct{} {
		err := provider.RegisterProvider("az", NewAzureProvider)
		if err != nil {
			panic(err)
		}
		return struct{}{}
	}()
)

// NewAzureProvider returns a new Azure provider.
func NewAzureProvider(config *provider.Config) provider.Provider {
	// register our prometheus metrics with the workqueue metric provider
	workqueueMetricsProviderInstance := new(metrics.WorkqueueMetricsProvider)
	workqueue.SetProvider(workqueueMetricsProviderInstance)

	return &Azure{
		config:           config,
		instanceStorage:  storage.NewMemoryStorage(),
		vmCapsStorage:    storage.NewMemoryStorage(),
		queue:            workqueue.NewNamedDelayingQueue("clients"),
		ClientAuthConfig: &DefaultAuthConfig{},
	}
}

// Run starts azure metrics gathering for all clusters returned by gardener.
func (a *Azure) Run(ctx context.Context, parentwg *sync.WaitGroup) {
	var wg sync.WaitGroup

	parentwg.Add(1)
	defer parentwg.Done()

	a.config.Logger.Info("starting provider")

	go a.clusterHandler(ctx)

	wg.Add(a.config.Workers)

	for i := 0; i < a.config.Workers; i++ {
		go func(i int) {
			defer wg.Done()

			for {
				// lock till a item is available from the queue.
				clusterid, quit := a.queue.Get()
				workerlogger := a.config.Logger.With("worker", i).With("cluster", clusterid)

				if quit {
					workerlogger.Debug("shutting down")
					return
				}

				obj, ok := a.instanceStorage.Get(clusterid.(string))
				if !ok {
					workerlogger.Warn("cluster not found in storage, must have been deleted")
					a.queue.Done(clusterid)

					continue
				}

				instance, ok := obj.(*Instance)
				if !ok {
					workerlogger.Errorf("object is the wrong type, should be Instance but got %T", obj)
					a.instanceStorage.Delete(clusterid.(string))
					a.queue.Done(clusterid)

					continue
				}

				workerlogger = workerlogger.With("account", instance.cluster.AccountID).With("subaccount", instance.cluster.SubAccountID)

				vmcaps := make(vmCapabilities)

				if obj, exists := a.vmCapsStorage.Get(instance.cluster.Region); exists {
					if caps, ok := obj.(*vmCapabilities); ok {
						vmcaps = *caps
					}
				} else {
					workerlogger.Warnf("vm capabilities for region %s not found, some metrics won't be available", instance.cluster.Region)
				}

				a.gatherMetrics(ctx, workerlogger, instance, &vmcaps)

				a.queue.Done(clusterid)

				// requeue item after X duration if client still in storage
				if !a.queue.ShuttingDown() {
					if _, exists := a.instanceStorage.Get(clusterid.(string)); exists {
						workerlogger.Debugf("requeuing cluster in %s", a.config.PollInterval)
						a.queue.AddAfter(clusterid, a.config.PollInterval)
					} else {
						workerlogger.Warn("can't requeue cluster, must have been deleted")
					}
				} else {
					workerlogger.Debug("queue is shutting down, can't requeue cluster")
				}
			}
		}(i)
	}

	wg.Wait()
	a.config.Logger.Info("stopping provider")
}

// clusterHandler listen on the cluster channel then update the storage and the queue.
func (a *Azure) clusterHandler(parentctx context.Context) {
	a.config.Logger.Debug("starting cluster handler")

	for {
		select {
		case cluster := <-a.config.ClusterChannel:
			logger := a.config.Logger.
				With("cluster", cluster.TechnicalID).
				With("account", cluster.AccountID).
				With("subaccount", cluster.SubAccountID)

			logger.Debug("received cluster from gardener controller")

			// if cluster was flag as deleted, remove it from storage and exit.
			if cluster.Deleted {
				logger.Info("removing cluster")
				metrics.StoredClusters.Dec()

				a.instanceStorage.Delete(cluster.TechnicalID)

				continue
			}

			// creating Azure REST API base client
			client, err := newClient(cluster, logger, a.config.ClientTraceLevel, a.ClientAuthConfig)
			if err != nil {
				logger.With("error", err).Error("error while creating client configuration")
				continue
			}

			instance := &Instance{cluster: cluster, client: client}

			// getting cluster and event hub resource group names.
			rg, err := client.GetResourceGroup(cluster.TechnicalID, "", logger)
			if err != nil {
				logger.Errorf("could not find cluster resource group, cluster may not be ready, retrying in %s: %s", a.config.PollInterval, err)
				time.AfterFunc(a.config.PollInterval, func() { a.config.ClusterChannel <- cluster })

				continue
			} else {
				instance.clusterResourceGroupName = *rg.Name
			}

			filter := fmt.Sprintf("tagname eq '%s' and tagvalue eq '%s'", tagNameSubAccountID, cluster.SubAccountID)

			rg, err = client.GetResourceGroup("", filter, logger)
			if err != nil {
				logger.Errorf("could not find event hub resource groups, cluster may not be ready, retrying in %s: %s", a.config.PollInterval, err)
				time.AfterFunc(a.config.PollInterval, func() { a.config.ClusterChannel <- cluster })

				continue
			} else {
				instance.eventHubResourceGroupName = *rg.Name
			}

			// recover the last event value if cluster already exists in storage.
			obj, exists := a.instanceStorage.Get(cluster.TechnicalID)
			if exists {
				if i, ok := obj.(*Instance); ok {
					instance.lastEvent = i.lastEvent
				}
			} else {
				metrics.StoredClusters.Inc()
			}

			// initialize vm capabilities cache for the cluster region if not already.
			_, exists = a.vmCapsStorage.Get(cluster.Region)
			if !exists {
				logger.Debugf("initializing vm capabilities cache for region %s", instance.cluster.Region)
				filter := fmt.Sprintf("location eq '%s'", cluster.Region)

				var vmcaps = make(vmCapabilities) // [vmtype][capname]capvalue

				skuList, err := instance.client.GetVMResourceSkus(parentctx, filter)
				if err != nil {
					logger.Errorf("error while getting vm capabilities for region %s: %s", cluster.Region, err)
					metrics.ClusterSyncFailureVec.WithLabelValues("vmcapabilities").Inc()
				} else {
					for _, item := range skuList {
						vmcaps[*item.Name] = make(map[string]string)
						for _, v := range *item.Capabilities {
							vmcaps[*item.Name][*v.Name] = *v.Value
						}
					}
				}

				if len(vmcaps) > 0 {
					a.vmCapsStorage.Put(instance.cluster.Region, &vmcaps)
				}
			}

			a.instanceStorage.Put(cluster.TechnicalID, instance)

			a.queue.Add(cluster.TechnicalID)
		case <-parentctx.Done():
			a.config.Logger.Debug("stopping cluster handler")
			a.queue.ShutDown()

			return
		}
	}
}

// gatherMetrics - collect results from different Azure API and create edp events.
func (a *Azure) gatherMetrics(parentctx context.Context, workerlogger *zap.SugaredLogger, instance *Instance, vmcaps *vmCapabilities) {
	var (
		cluster    = instance.cluster
		datatenant = cluster.SubAccountID
	)

	defer utils.TrackTime("gatherMetrics", time.Now(), workerlogger)

	resourceGroupName := instance.clusterResourceGroupName
	eventHubResourceGroupName := instance.eventHubResourceGroupName
	eventData := &EventData{ResourceGroups: []string{resourceGroupName, eventHubResourceGroupName}}

	metricTimer := prometheus.NewTimer(metrics.ReceivedSamplesDuration)
	defer metricTimer.ObserveDuration()

	workerlogger.Debug("getting metrics")

	// Using a timeout context to prevent azure api to hang for too long,
	// sometimes client get stuck waiting even with a max poll duration of 1 min.
	// If it reach the time limit, last successful event data will be returned.
	ctx, cancel := context.WithTimeout(parentctx, maxPollingDuration)
	defer cancel()

	eventData.Compute = instance.getComputeMetrics(ctx, resourceGroupName, workerlogger, vmcaps)
	eventData.Networking = instance.getNetworkMetrics(ctx, resourceGroupName, workerlogger)
	eventData.EventHub = instance.getEventHubMetrics(ctx, a.config.PollInterval, eventHubResourceGroupName, workerlogger)

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		workerlogger.Warn("Azure REST API call timedout, sending last successful event data")
		metrics.ProviderAPIErrorVec.WithLabelValues("timeout").Inc()

		if instance.lastEvent == nil {
			return
		}

		eventData = instance.lastEvent
	}

	eventDataRaw, err := json.Marshal(&eventData)
	if err != nil {
		workerlogger.Errorf("error parsing azure events to json, could not send event to EDP: %s", err)
		return
	}

	metrics.ReceivedSamples.Add(1)

	// save a copy of the event data in case of error next time
	instance.lastEvent = eventData

	eventDataJSON := json.RawMessage(eventDataRaw)

	eventBuffer := edp.Event{
		Datatenant: datatenant,
		Data:       &eventDataJSON,
	}

	a.config.EventsChannel <- &eventBuffer
}
