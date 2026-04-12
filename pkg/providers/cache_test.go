package providers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
	"gopkg.d7z.net/middleware/cache"
)

type cacheTestBackend struct{}

func (cacheTestBackend) Close() error { return nil }

func (cacheTestBackend) Meta(context.Context, string, string) (*core.Metadata, error) {
	return nil, nil
}

func (cacheTestBackend) Open(context.Context, string, string, string, string, http.Header) (*http.Response, error) {
	body := []byte("hello")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Length": []string{"5"},
			"Content-Type":   []string{"text/plain"},
		},
		Body: io.NopCloser(bytes.NewReader(body)),
	}, nil
}

type cacheRecorder struct {
	ttl time.Duration
}

func (c *cacheRecorder) Child(...string) cache.Cache { return c }

func (c *cacheRecorder) Put(_ context.Context, _ string, _ map[string]string, _ io.Reader, ttl time.Duration) error {
	c.ttl = ttl
	return nil
}

func (c *cacheRecorder) Get(context.Context, string) (*cache.Content, error) {
	return nil, os.ErrNotExist
}

func (c *cacheRecorder) Delete(context.Context, string) error { return nil }

func TestProviderCacheUsesConfiguredTTL(t *testing.T) {
	recorder := &cacheRecorder{}
	ttl := 3 * time.Minute
	provider := NewProviderCache(cacheTestBackend{}, recorder, 1024, ttl, 1, 1, time.Minute)

	resp, err := provider.Open(context.Background(), "org", "repo", "id", "index.html", nil)
	assert.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}

	assert.Equal(t, ttl, recorder.ttl)
}

func TestProviderCacheReturnsCachedContent(t *testing.T) {
	content := &cache.Content{
		ReadSeekCloser: utils.NopCloser{ReadSeeker: bytes.NewReader([]byte("cached"))},
		Metadata: map[string]string{
			"Content-Length": "6",
			"Content-Type":   "text/plain",
		},
	}
	recorder := &cacheRecorderWithContent{content: content}
	provider := NewProviderCache(cacheTestBackend{}, recorder, 1024, time.Minute, 1, 1, time.Minute)

	resp, err := provider.Open(context.Background(), "org", "repo", "id", "index.html", nil)
	assert.NoError(t, err)
	if assert.NotNil(t, resp) {
		defer resp.Body.Close()
		all, readErr := io.ReadAll(resp.Body)
		assert.NoError(t, readErr)
		assert.Equal(t, "cached", string(all))
	}
}

type cacheRecorderWithContent struct {
	content *cache.Content
}

func (c *cacheRecorderWithContent) Child(...string) cache.Cache { return c }

func (c *cacheRecorderWithContent) Put(context.Context, string, map[string]string, io.Reader, time.Duration) error {
	return nil
}

func (c *cacheRecorderWithContent) Get(context.Context, string) (*cache.Content, error) {
	return c.content, nil
}

func (c *cacheRecorderWithContent) Delete(context.Context, string) error { return nil }
