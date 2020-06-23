package provider

import (
	"context"
	"sync"
	"time"

	"github.com/kyma-incubator/compass/components/metris/internal/edp"
	"github.com/kyma-incubator/compass/components/metris/internal/gardener"
	"go.uber.org/zap"
)

// Factory generates a Provider.
type Factory func(config *Config) Provider

// Config holds providers base configuration.
type Config struct {
	Type             string        `kong:"help='Provider to fetch metrics from. (az)',enum='az',env='PROVIDER_TYPE',required=true,default='az',hidden=true"`
	PollInterval     time.Duration `kong:"help='Interval at which metrics are fetch.',env='PROVIDER_POLLINTERVAL',required=true,default='1m'"`
	Workers          int           `kong:"help='Number of workers to fetch metrics.',env='PROVIDER_WORKERS',required=true,default=10"`
	Buffer           int           `kong:"help='Number of cluster that the buffer can have.',env='PROVIDER_BUFFER',required=true,default=100"`
	ClientTraceLevel int           `kong:"help='Provider client trace level (0=disabled, 1=headers, 2=body)',env='PROVIDER_CLIENT_TRACE_LEVEL',default=0"`

	// ClusterChannel define the channel to exchange clusters information with Gardener controller.
	ClusterChannel chan *gardener.Cluster `kong:"-"`
	// EventsChannel define the channel to exchange events with EDP.
	EventsChannel chan<- *edp.Event `kong:"-"`
	// logger is the standard logger for the provider.
	Logger *zap.SugaredLogger `kong:"-"`
}

// Provider interface contains all behaviors for a provider.
type Provider interface {
	Run(ctx context.Context, wg *sync.WaitGroup)
}

// SecretMap is the interface that provides a method to decode kubenertes secrets into a Provider custom structure.
type SecretMap interface {
	decode(secrets map[string][]byte) error
}
