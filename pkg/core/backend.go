package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"code.d7z.net/d7z-project/gitea-pages/pkg/utils"
)

type Backend interface {
	// Repos return repo name + default branch
	Repos(owner string) (map[string]string, error)
	// Branches return branch + commit id
	Branches(owner, repo string) (map[string]string, error)
	// Open return file or error
	Open(client *http.Client, owner, repo, commit, path string, headers map[string]string) (*http.Response, error)
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
	data, err := c.config.Get(key)
	if err != nil {
		ret, err = c.backend.Repos(owner)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(data)
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
	return ret, nil
}

func (c *CacheBackend) Branches(owner, repo string) (map[string]string, error) {
	ret := make(map[string]string)
	key := fmt.Sprintf("branches/%s/%s", owner, repo)
	data, err := c.config.Get(key)
	if err != nil {
		ret, err = c.backend.Branches(owner, repo)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(data)
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
	return ret, nil
}

func (c *CacheBackend) Open(client *http.Client, owner, repo, commit, path string, headers map[string]string) (*http.Response, error) {
	return c.backend.Open(client, owner, repo, commit, path, headers)
}
