package core

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
	"gopkg.d7z.net/gitea-pages/pkg/utils"
	"gopkg.d7z.net/middleware/cache"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/tools"
)

type CacheBackend struct {
	backend     Backend
	cacheRepo   *tools.Cache[map[string]string]
	cacheBranch *tools.Cache[map[string]*BranchInfo]
}

func (c *CacheBackend) Close() error {
	return c.backend.Close()
}

func NewCacheBackend(backend Backend, cache kv.KV, ttl time.Duration) *CacheBackend {
	repoCache := tools.NewCache[map[string]string](cache, "repos", ttl)
	branchCache := tools.NewCache[map[string]*BranchInfo](cache, "branches", ttl)
	return &CacheBackend{
		backend:     backend,
		cacheRepo:   repoCache,
		cacheBranch: branchCache,
	}
}

func (c *CacheBackend) Repos(ctx context.Context, owner string) (map[string]string, error) {
	if load, b := c.cacheRepo.Load(ctx, owner); b {
		return load, nil
	}
	ret, err := c.backend.Repos(ctx, owner)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_ = c.cacheRepo.Store(ctx, owner, map[string]string{})
		}
		return nil, err
	}
	err = c.cacheRepo.Store(ctx, owner, ret)
	if len(ret) == 0 {
		return nil, os.ErrNotExist
	}
	return ret, err
}

func (c *CacheBackend) Branches(ctx context.Context, owner, repo string) (map[string]*BranchInfo, error) {
	key := fmt.Sprintf("%s/%s", owner, repo)
	if load, b := c.cacheBranch.Load(ctx, key); b {
		return load, nil
	}
	ret, err := c.backend.Branches(ctx, owner, repo)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_ = c.cacheBranch.Store(ctx, key, map[string]*BranchInfo{})
		}
		return nil, err
	}
	err = c.cacheBranch.Store(ctx, key, ret)
	if len(ret) == 0 {
		return nil, os.ErrNotExist
	}
	return ret, err
}

func (c *CacheBackend) Open(ctx context.Context, client *http.Client, owner, repo, commit, path string, headers http.Header) (*http.Response, error) {
	return c.backend.Open(ctx, client, owner, repo, commit, path, headers)
}

type CacheBackendBlobReader struct {
	client *http.Client
	cache  cache.Cache
	base   Backend
	limit  uint64
}

func NewCacheBackendBlobReader(
	client *http.Client,
	base Backend,
	cache cache.Cache,
	limit uint64,
) *CacheBackendBlobReader {
	return &CacheBackendBlobReader{client: client, base: base, cache: cache, limit: limit}
}

func (c *CacheBackendBlobReader) Open(ctx context.Context, owner, repo, commit, path string) (io.ReadCloser, error) {
	key := fmt.Sprintf("%s/%s/%s/%s", owner, repo, commit, path)
	lastCache, err := c.cache.Get(ctx, key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if lastCache == nil && err == nil {
		// 边界缓存
		return nil, os.ErrNotExist
	} else if lastCache != nil {
		return lastCache, nil
	}
	open, err := c.base.Open(ctx, c.client, owner, repo, commit, path, http.Header{})
	if err != nil || open == nil {
		if open != nil {
			_ = open.Body.Close()
		}
		return nil, err
	}
	if open.StatusCode == http.StatusNotFound {
		// 缓存 404 路由
		_ = c.cache.Put(ctx, key, nil, time.Hour)
		_ = open.Body.Close()
		return nil, os.ErrNotExist
	}

	lastMod, err := time.Parse(http.TimeFormat, open.Header.Get("Last-Modified"))
	if err != nil {
		// 无时间，跳过
		return open.Body, nil
	}
	length, err := strconv.ParseUint(open.Header.Get("Content-Length"), 10, 64)
	// 无法计算大小，跳过
	if err != nil {
		return open.Body, nil
	}
	if length > c.limit {
		// 超过最大大小，跳过
		return &utils.SizeReadCloser{
			ReadCloser: open.Body,
			Size:       length,
		}, nil
	}

	defer open.Body.Close()
	allBytes, err := io.ReadAll(open.Body)
	if err != nil {
		return nil, err
	}
	if err = c.cache.Put(ctx, key, bytes.NewBuffer(allBytes), time.Hour); err != nil {
		zap.L().Warn("缓存归档失败", zap.Error(err), zap.Int("Size", len(allBytes)), zap.Uint64("MaxSize", c.limit))
	}
	return &cache.Content{
		ReadSeekCloser: utils.NopCloser{
			ReadSeeker: bytes.NewReader(allBytes),
		},
		LastModified: lastMod,
		Length:       length,
	}, nil
}
