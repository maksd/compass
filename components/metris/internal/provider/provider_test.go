package provider

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

type fakeTestProvider struct{}

func NewTestProvider(config *Config) Provider {
	return &fakeTestProvider{}
}

func (a *fakeTestProvider) Run(ctx context.Context, parentwg *sync.WaitGroup) {}

func TestNewProvider(t *testing.T) {
	asserts := assert.New(t)

	t.Run("get unregistered provider", func(t *testing.T) {
		_, err := GetProvider(&Config{Type: "test", Logger: zap.NewNop().Sugar()})
		asserts.Error(err, "should return an error")
	})

	t.Run("register provider", func(t *testing.T) {
		err := RegisterProvider("test", NewTestProvider)
		asserts.NoError(err, "should not return an error")
	})

	t.Run("register provider twice", func(t *testing.T) {
		err := RegisterProvider("test", NewTestProvider)
		asserts.Error(err, "should return an error")
	})

	t.Run("get registered provider", func(t *testing.T) {
		p, err := GetProvider(&Config{Type: "test", Logger: zap.NewNop().Sugar()})
		asserts.NoError(err, "should not return an error")
		asserts.Implements((*Provider)(nil), p, "should implement Provider interface")
	})
}
