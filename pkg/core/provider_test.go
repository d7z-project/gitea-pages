package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testProviderFactoryStub struct{}

func (testProviderFactoryStub) Close() error { return nil }
func (testProviderFactoryStub) Meta(context.Context, string, string) (*Metadata, error) {
	return &Metadata{ID: "1", LastModified: time.Now()}, nil
}

func (testProviderFactoryStub) Open(context.Context, string, string, string, string, http.Header) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
}

func (testProviderFactoryStub) List(context.Context, string, string, string, string) ([]DirEntry, error) {
	return nil, nil
}

func TestProviderRegistry(t *testing.T) {
	RegisterProvider("test-provider-registry", func(_ *http.Client, raw json.RawMessage, options ProviderOptions) (Provider, error) {
		assert.JSONEq(t, `{"enabled":true}`, string(raw))
		assert.Equal(t, "gh-pages", options.DefaultBranch)
		return testProviderFactoryStub{}, nil
	})

	factory, ok := GetProviderFactory("test-provider-registry")
	require.True(t, ok)

	provider, err := factory(http.DefaultClient, json.RawMessage(`{"enabled":true}`), ProviderOptions{
		DefaultBranch: "gh-pages",
	})
	require.NoError(t, err)
	require.NotNil(t, provider)
}
