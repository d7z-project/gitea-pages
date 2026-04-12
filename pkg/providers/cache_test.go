package providers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"sync"
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

func (cacheTestBackend) List(context.Context, string, string, string, string) ([]core.DirEntry, error) {
	return []core.DirEntry{{Name: "index.html", Path: "index.html", Type: "file", Size: 5}}, nil
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
	provider := NewProviderCache(cacheTestBackend{}, recorder, 1024, ttl, 2*time.Minute, 1, 1, time.Minute, 4*time.Minute)

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
	provider := NewProviderCache(cacheTestBackend{}, recorder, 1024, time.Minute, 2*time.Minute, 1, 1, time.Minute, 4*time.Minute)

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

type countingListBackend struct {
	cacheTestBackend
	mu        sync.Mutex
	listCalls int
}

func (b *countingListBackend) List(context.Context, string, string, string, string) ([]core.DirEntry, error) {
	b.mu.Lock()
	b.listCalls++
	b.mu.Unlock()
	return []core.DirEntry{
		{Name: "docs", Path: "docs", Type: "dir"},
		{Name: "index.html", Path: "index.html", Type: "file", Size: 5},
	}, nil
}

func TestProviderCacheCachesDirectoryEntries(t *testing.T) {
	recorder := newMemoryCacheRecorder()
	backend := &countingListBackend{}
	provider := NewProviderCache(backend, recorder, 1024, time.Minute, 5*time.Minute, 1, 1, time.Minute, 2*time.Minute)

	first, err := provider.List(context.Background(), "org", "repo", "id", "docs")
	assert.NoError(t, err)
	second, err := provider.List(context.Background(), "org", "repo", "id", "docs")
	assert.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Equal(t, 1, backend.listCalls)
}

type notFoundListBackend struct {
	cacheTestBackend
	mu        sync.Mutex
	listCalls int
}

func (b *notFoundListBackend) List(context.Context, string, string, string, string) ([]core.DirEntry, error) {
	b.mu.Lock()
	b.listCalls++
	b.mu.Unlock()
	return nil, os.ErrNotExist
}

func TestProviderCacheCachesDirectoryNotFound(t *testing.T) {
	recorder := newMemoryCacheRecorder()
	backend := &notFoundListBackend{}
	provider := NewProviderCache(backend, recorder, 1024, time.Minute, 5*time.Minute, 1, 1, time.Minute, 2*time.Minute)

	_, err := provider.List(context.Background(), "org", "repo", "id", "missing")
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = provider.List(context.Background(), "org", "repo", "id", "missing")
	assert.ErrorIs(t, err, os.ErrNotExist)

	assert.Equal(t, 1, backend.listCalls)
}

type memoryCacheRecorder struct {
	mu      sync.Mutex
	content map[string]*cache.Content
}

func newMemoryCacheRecorder() *memoryCacheRecorder {
	return &memoryCacheRecorder{content: map[string]*cache.Content{}}
}

func (c *memoryCacheRecorder) Child(...string) cache.Cache { return c }

func (c *memoryCacheRecorder) Put(_ context.Context, key string, metadata map[string]string, data io.Reader, _ time.Duration) error {
	all, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.content[key] = &cache.Content{
		ReadSeekCloser: utils.NopCloser{ReadSeeker: bytes.NewReader(all)},
		Metadata:       metadata,
	}
	return nil
}

func (c *memoryCacheRecorder) Get(_ context.Context, key string) (*cache.Content, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	content, ok := c.content[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	var payload []byte
	if content != nil {
		_, _ = content.Seek(0, io.SeekStart)
		payload, _ = io.ReadAll(content)
		_, _ = content.Seek(0, io.SeekStart)
	}
	metadata := map[string]string{}
	for k, v := range content.Metadata {
		metadata[k] = v
	}
	return &cache.Content{
		ReadSeekCloser: utils.NopCloser{ReadSeeker: bytes.NewReader(payload)},
		Metadata:       metadata,
	}, nil
}

func (c *memoryCacheRecorder) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.content, key)
	return nil
}
