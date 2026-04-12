package core

import (
	"encoding/json"
	"net/http"
	"sync"
)

type Provider interface {
	Backend
}

type ProviderWithAuth interface {
	Provider
	AuthProvider
	AuthEnabled() bool
}

type ProviderOptions struct {
	DefaultBranch string
}

type ProviderFactory func(httpClient *http.Client, raw json.RawMessage, options ProviderOptions) (Provider, error)

var providerRegistry sync.Map

func RegisterProvider(name string, factory ProviderFactory) {
	providerRegistry.Store(name, factory)
}

func GetProviderFactory(name string) (ProviderFactory, bool) {
	value, ok := providerRegistry.Load(name)
	if !ok {
		return nil, false
	}
	factory, ok := value.(ProviderFactory)
	return factory, ok
}
