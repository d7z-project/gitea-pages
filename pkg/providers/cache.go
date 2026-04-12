package providers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
	"gopkg.d7z.net/middleware/cache"
)

type ProviderCache struct {
	parent core.Backend

	cacheBlob      cache.Cache
	cacheBlobLimit uint64
	cacheBlobTTL   time.Duration
	cacheSem       chan struct{}
	backendSem     chan struct{}
	notFoundTTL    time.Duration
}

func (c *ProviderCache) Close() error {
	return c.parent.Close()
}

func NewProviderCache(
	backend core.Backend,
	cacheBlob cache.Cache,
	cacheBlobLimit uint64,
	cacheBlobTTL time.Duration,
	cacheConcurrent uint64,
	backendConcurrent uint64,
	notFoundTTL time.Duration,
) *ProviderCache {
	if cacheConcurrent == 0 {
		cacheConcurrent = 16 // 默认限制 16 个并发缓存操作
	}
	if backendConcurrent == 0 {
		backendConcurrent = 64 // 默认限制 64 个并发后端请求
	}
	if notFoundTTL == 0 {
		notFoundTTL = time.Hour // 默认 404 缓存 1 小时
	}
	return &ProviderCache{
		parent:         backend,
		cacheBlob:      cacheBlob,
		cacheBlobLimit: cacheBlobLimit,
		cacheBlobTTL:   cacheBlobTTL,
		cacheSem:       make(chan struct{}, cacheConcurrent),
		backendSem:     make(chan struct{}, backendConcurrent),
		notFoundTTL:    notFoundTTL,
	}
}

func (c *ProviderCache) Meta(ctx context.Context, owner, repo string) (*core.Metadata, error) {
	// 获取后端并发锁
	select {
	case c.backendSem <- struct{}{}:
		defer func() { <-c.backendSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return c.parent.Meta(ctx, owner, repo)
}

func (c *ProviderCache) Open(ctx context.Context, owner, repo, id, path string, headers http.Header) (*http.Response, error) {
	if headers != nil && headers.Get("Range") != "" {
		return c.parent.Open(ctx, owner, repo, id, path, headers)
	}
	key := c.cacheKey(owner, repo, id, path)
	if resp, err := c.loadCachedResponse(ctx, key); resp != nil || err != nil {
		return resp, err
	}

	releaseBackend, err := c.acquireBackend(ctx)
	if err != nil {
		return nil, err
	}

	open, err := c.parent.Open(ctx, owner, repo, id, path, http.Header{})
	if err != nil || open == nil {
		releaseBackend()
		if open != nil {
			_ = open.Body.Close()
		}
		return nil, c.handleBackendError(ctx, key, err)
	}

	return c.handleBackendResponse(ctx, key, path, open, releaseBackend)
}

func (c *ProviderCache) cacheKey(owner, repo, id, path string) string {
	return fmt.Sprintf("%s/%s/%s/%s", owner, repo, id, path)
}

func (c *ProviderCache) loadCachedResponse(ctx context.Context, key string) (*http.Response, error) {
	content, err := c.cacheBlob.Get(ctx, key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if content == nil {
		return nil, os.ErrNotExist
	}
	if content.Metadata["404"] == "true" {
		return nil, os.ErrNotExist
	}

	length, err := strconv.Atoi(content.Metadata["Content-Length"])
	if err != nil {
		return nil, err
	}

	header := make(http.Header)
	header.Set("Last-Modified", content.Metadata["Last-Modified"])
	header.Set("Content-Type", content.Metadata["Content-Type"])
	header.Set("Content-Length", content.Metadata["Content-Length"])
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Body:          content,
		ContentLength: int64(length),
		Header:        header,
	}, nil
}

func (c *ProviderCache) acquireBackend(ctx context.Context) (func(), error) {
	select {
	case c.backendSem <- struct{}{}:
		return func() { <-c.backendSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *ProviderCache) handleBackendError(ctx context.Context, key string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		c.cacheNotFound(ctx, key)
	}
	return err
}

func (c *ProviderCache) handleBackendResponse(ctx context.Context, key, path string, resp *http.Response, releaseBackend func()) (*http.Response, error) {
	resp.Body = &utils.CloserWrapper{
		ReadCloser: resp.Body,
		OnClose:    releaseBackend,
	}

	if resp.StatusCode == http.StatusNotFound {
		c.cacheNotFound(ctx, key)
		_ = resp.Body.Close()
		return nil, os.ErrNotExist
	}

	length, err := strconv.ParseUint(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		return resp, nil
	}
	if length > c.cacheBlobLimit {
		return c.streamResponse(resp, length), nil
	}
	if !c.tryAcquireCacheSlot() {
		zap.L().Debug("跳过缓存，并发限制已达", zap.String("path", path))
		return c.streamResponse(resp, length), nil
	}
	defer c.releaseCacheSlot()

	return c.cacheResponse(ctx, key, resp)
}

func (c *ProviderCache) cacheNotFound(ctx context.Context, key string) {
	if err := c.cacheBlob.Put(ctx, key, map[string]string{
		"404": "true",
	}, bytes.NewBuffer(nil), c.notFoundTTL); err != nil {
		zap.L().Warn("缓存404失败", zap.Error(err))
	}
}

func (c *ProviderCache) streamResponse(resp *http.Response, size uint64) *http.Response {
	resp.Body = &utils.SizeReadCloser{
		ReadCloser: resp.Body,
		Size:       size,
	}
	return resp
}

func (c *ProviderCache) tryAcquireCacheSlot() bool {
	select {
	case c.cacheSem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (c *ProviderCache) releaseCacheSlot() {
	<-c.cacheSem
}

func (c *ProviderCache) cacheResponse(ctx context.Context, key string, resp *http.Response) (*http.Response, error) {
	defer resp.Body.Close()

	allBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err = c.cacheBlob.Put(ctx, key, map[string]string{
		"Content-Length": resp.Header.Get("Content-Length"),
		"Last-Modified":  resp.Header.Get("Last-Modified"),
		"Content-Type":   resp.Header.Get("Content-Type"),
	}, bytes.NewBuffer(allBytes), c.cacheBlobTTL); err != nil {
		zap.L().Warn("缓存归档失败", zap.Error(err), zap.Int("Size", len(allBytes)), zap.Uint64("MaxSize", c.cacheBlobLimit))
	}

	resp.Body = utils.NopCloser{
		ReadSeeker: bytes.NewReader(allBytes),
	}
	return resp, nil
}
