package core

import (
	"bytes"
	"encoding/json"
	stdErr "errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"code.d7z.net/d7z-project/gitea-pages/pkg/utils"
)

type BranchInfo struct {
	ID           string    `json:"id"`
	LastModified time.Time `json:"last_modified"`
}

type Backend interface {
	// Repos return repo name + default branch
	Repos(owner string) (map[string]string, error)
	// Branches return branch + commit id
	Branches(owner, repo string) (map[string]*BranchInfo, error)
	// Open return file or error (error)
	Open(client *http.Client, owner, repo, commit, path string, headers http.Header) (*http.Response, error)
}

type CacheBackend struct {
	backend Backend
	config  utils.Config
	ttl     time.Duration
}

func NewCacheBackend(backend Backend, config utils.Config, ttl time.Duration) *CacheBackend {
	return &CacheBackend{backend: backend, config: config, ttl: ttl}
}

func (c *CacheBackend) Repos(owner string) (map[string]string, error) {
	ret := make(map[string]string)
	key := fmt.Sprintf("repos/%s", owner)
	store, err := c.config.Get(key)
	if err != nil {
		ret, err = c.backend.Repos(owner)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				_ = c.config.Put(key, "{}", c.ttl)
			}
			return nil, err
		}
		storeBin, err := json.Marshal(ret)
		if err != nil {
			return nil, err
		}
		if err = c.config.Put(key, string(storeBin), c.ttl); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal([]byte(store), &ret); err != nil {
			return nil, err
		}
	}
	if len(ret) == 0 {
		return ret, os.ErrNotExist
	}
	return ret, nil
}

func (c *CacheBackend) Branches(owner, repo string) (map[string]*BranchInfo, error) {
	ret := make(map[string]*BranchInfo)
	key := fmt.Sprintf("branches/%s/%s", owner, repo)
	data, err := c.config.Get(key)
	if err != nil {
		ret, err = c.backend.Branches(owner, repo)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				_ = c.config.Put(key, "{}", c.ttl)
			}
			return nil, err
		}
		data, err := json.Marshal(ret)
		if err != nil {
			return nil, err
		}
		if err = c.config.Put(key, string(data), c.ttl); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal([]byte(data), &ret); err != nil {
			return nil, err
		}
	}
	if len(ret) == 0 {
		return ret, os.ErrNotExist
	}
	return ret, nil
}

func (c *CacheBackend) Open(client *http.Client, owner, repo, commit, path string, headers http.Header) (*http.Response, error) {
	return c.backend.Open(client, owner, repo, commit, path, headers)
}

type CacheBackendBlobReader struct {
	client  *http.Client
	cache   utils.Cache
	base    Backend
	maxSize int
}

func NewCacheBackendBlobReader(client *http.Client, base Backend, cache utils.Cache, maxCacheSize int) *CacheBackendBlobReader {
	return &CacheBackendBlobReader{client: client, base: base, cache: cache, maxSize: maxCacheSize}
}

func (c *CacheBackendBlobReader) Open(owner, repo, commit, path string) (io.ReadCloser, error) {
	key := fmt.Sprintf("%s/%s/%s/%s", owner, repo, commit, path)
	lastCache, err := c.cache.Get(key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if lastCache == nil && err == nil {
		// 边界缓存
		return nil, os.ErrNotExist
	} else if lastCache != nil {
		return lastCache, nil
	}
	open, err := c.base.Open(c.client, owner, repo, commit, path, http.Header{})
	if err != nil || open == nil {
		if open != nil {
			_ = open.Body.Close()
		}
		return nil, stdErr.Join(err, os.ErrNotExist)
	}
	if open.StatusCode == http.StatusNotFound {
		// 缓存 404 路由
		_ = c.cache.Put(key, nil)
		_ = open.Body.Close()
		return nil, os.ErrNotExist
	}

	lastMod, err := time.Parse(http.TimeFormat, open.Header.Get("Last-Modified"))
	if err != nil {
		// 无时间，跳过
		return open.Body, nil
	}
	// 没法计算大小，跳过
	length, err := strconv.Atoi(open.Header.Get("Content-Length"))
	if err != nil {
		return open.Body, err
	}
	if length > c.maxSize {
		// 超过最大大小，跳过
		return &utils.SizeReadCloser{
			ReadCloser: open.Body,
			Size:       length,
		}, err
	}
	defer open.Body.Close()
	allBytes, err := io.ReadAll(open.Body)
	if err != nil {
		return nil, err
	}
	if err = c.cache.Put(key, bytes.NewBuffer(allBytes)); err != nil {
		zap.L().Warn("缓存归档失败", zap.Error(err), zap.Int("Size", len(allBytes)), zap.Int("MaxSize", c.maxSize))
	}
	return &utils.CacheContent{
		ReadSeekCloser: utils.NopCloser{
			ReadSeeker: bytes.NewReader(allBytes),
		},
		LastModified: lastMod,
		Length:       length,
	}, nil
}
