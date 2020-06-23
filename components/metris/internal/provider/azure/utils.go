package azure

import (
	"net/http"
	"net/http/httputil"

	"github.com/Azure/go-autorest/autorest"
	"github.com/kyma-incubator/compass/components/metris/internal/metrics"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LogRequest inspect the requests being made to Azure and writes it to the logger.
func LogRequest(logger *zap.SugaredLogger, tracelevel int) autorest.PrepareDecorator {
	return func(p autorest.Preparer) autorest.Preparer {
		return autorest.PreparerFunc(func(r *http.Request) (*http.Request, error) {
			r, err := p.Prepare(r)
			if err == nil && tracelevel > 0 && logger.Desugar().Core().Enabled(zapcore.DebugLevel) {
				if d, e := httputil.DumpRequest(r, tracelevel > 1); e == nil {
					// remove Authorization header??
					logger.Debug(string(d))
				}
			}

			return r, err
		})
	}
}

// LogResponse inspect the response received from Azure and writes it to the logger.
func LogResponse(logger *zap.SugaredLogger, tracelevel int) autorest.RespondDecorator {
	return func(r autorest.Responder) autorest.Responder {
		return autorest.ResponderFunc(func(resp *http.Response) error {
			err := r.Respond(resp)
			if err == nil && resp != nil {
				if resp.Header != nil {
					retry := resp.Header[http.CanonicalHeaderKey("Retry-After")]
					if len(retry) > 0 {
						logger.Warnf("Azure API client got a throttling error, retry-after %s: %s", retry[0], resp.Request.URL.String())
						metrics.ProviderAPIErrorVec.WithLabelValues("throttling").Inc()
					}
				}

				if tracelevel > 0 && logger.Desugar().Core().Enabled(zapcore.DebugLevel) {
					if dump, e := httputil.DumpResponse(resp, tracelevel > 1); e == nil {
						logger.Debug(string(dump))
					}
				}
			}

			return err
		})
	}
}
