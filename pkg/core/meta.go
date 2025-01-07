package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"code.d7z.net/d7z-project/gitea-pages/pkg/utils"
)

type ServerMeta struct {
	Backend

	client *http.Client
	cache  utils.Config
	ttl    time.Duration

	locker *utils.Locker
}

type PageMetaContent struct {
	CommitID         string    `json:"id"`  // 提交 COMMIT ID
	IsPage           bool      `json:"pg"`  // 是否为 Page
	Domain           string    `json:"dm"`  // 匹配的域名
	HistoryRouteMode bool      `json:"rt"`  // 路由模式
	CustomNotFound   bool      `json:"404"` // 注册了自定义 404 页面
	LastModified     time.Time `json:"up"`  // 上次更新时间
}

func (m *PageMetaContent) From(data string) error {
	return json.Unmarshal([]byte(data), m)
}

func (m *PageMetaContent) String() string {
	marshal, _ := json.Marshal(m)
	return string(marshal)
}

func NewServerMeta(client *http.Client, backend Backend, config utils.Config, ttl time.Duration) *ServerMeta {
	return &ServerMeta{backend, client, config, ttl, utils.NewLocker()}
}

func (s *ServerMeta) GetMeta(owner, repo, branch string) (*PageMetaContent, error) {
	rel := &PageMetaContent{
		IsPage: false,
	}
	if repos, err := s.Repos(owner); err != nil {
		return nil, err
	} else {
		defBranch := repos[repo]
		if defBranch == "" {
			return nil, os.ErrNotExist
		}
		if branch == "" {
			branch = defBranch
		}
	}
	if branches, err := s.Branches(owner, repo); err != nil {
		return nil, err
	} else {
		info := branches[branch]
		if info == nil {
			return nil, os.ErrNotExist
		}
		rel.CommitID = info.ID
		rel.LastModified = info.LastModified
	}

	key := fmt.Sprintf("meta/%s/%s/%s", owner, repo, branch)
	cache, err := s.cache.Get(key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err = rel.From(cache); err == nil {
			if !rel.IsPage {
				return nil, os.ErrNotExist
			}
			return rel, nil
		}
	}
	mux := s.locker.Open(key)
	mux.Lock()
	defer mux.Unlock()
	cache, err = s.cache.Get(key)
	if err == nil {
		if err = rel.From(cache); err == nil {
			if !rel.IsPage {
				return nil, os.ErrNotExist
			}
			return rel, nil
		}
	}

	if find, _ := s.FileExists(owner, repo, rel.CommitID, "index.html"); !find {
		rel.IsPage = false
		_ = s.cache.Put(key, rel.String(), s.ttl)
		return nil, os.ErrNotExist
	}
	if find, _ := s.FileExists(owner, repo, rel.CommitID, "404.html"); !find {
		rel.CustomNotFound = true
	}
	if cname, err := s.ReadString(owner, repo, rel.CommitID, "CNAME"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else {
		rel.Domain = strings.TrimSpace(cname)
	}
	if find, _ := s.FileExists(owner, repo, rel.CommitID, ".history"); find {
		rel.HistoryRouteMode = true
	}
	_ = s.cache.Put(key, rel.String(), s.ttl)
	return rel, nil
}

func (s *ServerMeta) ReadString(owner, repo, branch, path string) (string, error) {
	resp, err := s.Open(s.client, owner, repo, branch, path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", os.ErrNotExist
	}
	all, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(all), nil
}

func (s *ServerMeta) FileExists(owner, repo, branch, path string) (bool, error) {
	resp, err := s.Open(s.client, owner, repo, branch, path, nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	return false, nil
}
