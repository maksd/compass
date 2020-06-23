package provider

import (
	"fmt"
	"sync"
)

// All registered providers.
var (
	providersLock sync.Mutex
	providers     = make(map[string]Factory)
)

func RegisterProvider(name string, provider Factory) error {
	providersLock.Lock()
	defer providersLock.Unlock()

	if _, exists := providers[name]; exists {
		return fmt.Errorf("Provider %s is already registered", name)
	}

	providers[name] = provider

	return nil
}

// GetProvider returns a registered provider base on the type.
func GetProvider(config *Config) (Provider, error) {
	config.Logger.Debug("initializing provider")

	providersLock.Lock()
	defer providersLock.Unlock()

	p, ok := providers[config.Type]
	if !ok {
		return nil, fmt.Errorf("no provider found with name %s", config.Type)
	}

	return p(config), nil
}
