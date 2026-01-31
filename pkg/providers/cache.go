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
	cacheSem       chan struct{}
	backendSem     chan struct{}
}

func (c *ProviderCache) Close() error {
	return c.parent.Close()
}

func NewProviderCache(
	backend core.Backend,
	cacheBlob cache.Cache,
	cacheBlobLimit uint64,
	cacheConcurrent uint64,
	backendConcurrent uint64,
) *ProviderCache {
	if cacheConcurrent == 0 {
		cacheConcurrent = 16 // 默认限制 16 个并发缓存操作
	}
	if backendConcurrent == 0 {
		backendConcurrent = 64 // 默认限制 64 个并发后端请求
	}
	return &ProviderCache{
		parent:         backend,
		cacheBlob:      cacheBlob,
		cacheBlobLimit: cacheBlobLimit,
		cacheSem:       make(chan struct{}, cacheConcurrent),
		backendSem:     make(chan struct{}, backendConcurrent),
	}
}

func (c *ProviderCache) Meta(ctx context.Context, owner, repo string) (*core.Metadata, error) {
	return c.parent.Meta(ctx, owner, repo)
}

func (c *ProviderCache) Open(ctx context.Context, owner, repo, id, path string, headers http.Header) (*http.Response, error) {
	if headers != nil && headers.Get("Range") != "" {
		// ignore custom header
		return c.parent.Open(ctx, owner, repo, id, path, headers)
	}
	key := fmt.Sprintf("%s/%s/%s/%s", owner, repo, id, path)
	lastCache, err := c.cacheBlob.Get(ctx, key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if lastCache == nil && err == nil {
		// 边界缓存
		return nil, os.ErrNotExist
	} else if lastCache != nil {
		h := lastCache.Metadata
		if h["404"] == "true" {
			return nil, os.ErrNotExist
		}
		respHeader := make(http.Header)
		respHeader.Set("Last-Modified", h["Last-Modified"])
		respHeader.Set("Content-Type", h["Content-Type"])
		respHeader.Set("Content-Length", h["Content-Length"])
		atoi, err := strconv.Atoi(h["Content-Length"])
		if err != nil {
			return nil, err
		}
		return &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Body:          lastCache,
			ContentLength: int64(atoi),
			Request:       nil,
			Header:        respHeader,
		}, nil
	}

	// 获取后端并发锁
	select {
	case c.backendSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	releaseBackend := func() { <-c.backendSem }
	success := false
	defer func() {
		if !success {
			releaseBackend()
		}
	}()

	open, err := c.parent.Open(ctx, owner, repo, id, path, http.Header{})
	if err != nil || open == nil {
		if open != nil {
			_ = open.Body.Close()
		}
		// 当上游返回错误时，缓存404结果
		if errors.Is(err, os.ErrNotExist) {
			if err = c.cacheBlob.Put(ctx, key, map[string]string{
				"404": "true",
			}, bytes.NewBuffer(nil), time.Hour); err != nil {
				zap.L().Warn("缓存404失败", zap.Error(err))
			}
		}
		return nil, err
	}

	// 包装 Body 以在关闭时释放信号量
	open.Body = &utils.CloserWrapper{
		ReadCloser: open.Body,
		OnClose:    releaseBackend,
	}
	success = true

	if open.StatusCode == http.StatusNotFound {
		// 缓存404路由
		if err = c.cacheBlob.Put(ctx, key, map[string]string{
			"404": "true",
		}, bytes.NewBuffer(nil), time.Hour); err != nil {
			zap.L().Warn("缓存404失败", zap.Error(err))
		}
		_ = open.Body.Close()
		return nil, os.ErrNotExist
	}
	length, err := strconv.ParseUint(open.Header.Get("Content-Length"), 10, 64)
	// 无法计算大小，跳过
	if err != nil {
		return open, nil
	}
	if length > c.cacheBlobLimit {
		// 超过最大大小，跳过
		open.Body = &utils.SizeReadCloser{
			ReadCloser: open.Body,
			Size:       length,
		}
		return open, nil
	}

	// 尝试获取信号量进行缓存
	select {
	case c.cacheSem <- struct{}{}:
		defer func() { <-c.cacheSem }()
		defer open.Body.Close()
		allBytes, err := io.ReadAll(open.Body)
		if err != nil {
			return nil, err
		}
		if err = c.cacheBlob.Put(ctx, key, map[string]string{
			"Content-Length": open.Header.Get("Content-Length"),
			"Last-Modified":  open.Header.Get("Last-Modified"),
			"Content-Type":   open.Header.Get("Content-Type"),
		}, bytes.NewBuffer(allBytes), time.Hour); err != nil {
			zap.L().Warn("缓存归档失败", zap.Error(err), zap.Int("Size", len(allBytes)), zap.Uint64("MaxSize", c.cacheBlobLimit))
		}
		open.Body = utils.NopCloser{
			ReadSeeker: bytes.NewReader(allBytes),
		}
		return open, nil
	default:
		// 无法获取信号量，直接流式返回，不进行缓存
		zap.L().Debug("跳过缓存，并发限制已达", zap.String("path", path))
		open.Body = &utils.SizeReadCloser{
			ReadCloser: open.Body,
			Size:       length,
		}
		return open, nil
	}
}
