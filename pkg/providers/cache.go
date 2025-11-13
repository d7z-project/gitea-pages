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
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/tools"
)

type ProviderCache struct {
	parent      core.Backend
	cacheRepo   *tools.Cache[map[string]string]
	cacheBranch *tools.Cache[map[string]*core.BranchInfo]

	cacheBlob      cache.Cache
	cacheBlobLimit uint64
}

func (c *ProviderCache) Close() error {
	return c.parent.Close()
}

func NewProviderCache(
	backend core.Backend,
	cacheMeta kv.KV,
	cacheMetaTTL time.Duration,
	cacheBlob cache.Cache,
	cacheBlobLimit uint64,
) *ProviderCache {
	repoCache := tools.NewCache[map[string]string](cacheMeta, "repos", cacheMetaTTL)
	branchCache := tools.NewCache[map[string]*core.BranchInfo](cacheMeta, "branches", cacheMetaTTL)
	return &ProviderCache{
		parent:      backend,
		cacheRepo:   repoCache,
		cacheBranch: branchCache,

		cacheBlob:      cacheBlob,
		cacheBlobLimit: cacheBlobLimit,
	}
}

func (c *ProviderCache) Repos(ctx context.Context, owner string) (map[string]string, error) {
	if load, b := c.cacheRepo.Load(ctx, owner); b {
		return load, nil
	}
	ret, err := c.parent.Repos(ctx, owner)
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

func (c *ProviderCache) Branches(ctx context.Context, owner, repo string) (map[string]*core.BranchInfo, error) {
	key := fmt.Sprintf("%s/%s", owner, repo)
	if load, b := c.cacheBranch.Load(ctx, key); b {
		return load, nil
	}
	ret, err := c.parent.Branches(ctx, owner, repo)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_ = c.cacheBranch.Store(ctx, key, map[string]*core.BranchInfo{})
		}
		return nil, err
	}
	err = c.cacheBranch.Store(ctx, key, ret)
	if len(ret) == 0 {
		return nil, os.ErrNotExist
	}
	return ret, err
}

func (c *ProviderCache) Open(ctx context.Context, client *http.Client, owner, repo, commit, path string, headers http.Header) (*http.Response, error) {
	if headers != nil && headers.Get("Range") != "" {
		// ignore custom header
		return c.parent.Open(ctx, client, owner, repo, commit, path, headers)
	}
	key := fmt.Sprintf("%s/%s/%s/%s", owner, repo, commit, path)
	lastCache, err := c.cacheBlob.Get(ctx, key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if lastCache == nil && err == nil {
		// 边界缓存
		return nil, os.ErrNotExist
	} else if lastCache != nil {
		h := lastCache.Metadata
		if h["Not-Found"] == "true" {
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
	open, err := c.parent.Open(ctx, client, owner, repo, commit, path, http.Header{})
	if err != nil || open == nil {
		if open != nil {
			_ = open.Body.Close()
		}
		return nil, err
	}
	if open.StatusCode == http.StatusNotFound {
		// TODO: 缓存 404 路由
		//_ = c.cache.Put(ctx, key, nil, time.Hour)
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
}
