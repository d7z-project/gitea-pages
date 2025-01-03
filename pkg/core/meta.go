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
	client  *http.Client
	backend Backend
	cache   utils.Config
	ttl     time.Duration
}

type PageMeta struct {
	CommitID         string `json:"commit_id"`     // 提交 COMMIT ID
	IsPage           bool   `json:"is_page"`       // 是否为 Page
	Domain           string `json:"domain"`        // 匹配的域名和路径
	HistoryRouteMode bool   `json:"route_history"` // 路由模式
}

func NewServerMeta(client *http.Client, backend Backend, config utils.Config, ttl time.Duration) *ServerMeta {
	return &ServerMeta{client, backend, config, ttl}
}

func (s *ServerMeta) Meta(owner, repo, branch string) (*PageMeta, error) {
	rel := &PageMeta{
		IsPage: false,
	}
	key := fmt.Sprintf("meta/%s/%s/%s", owner, repo, branch)
	pushMeta := func() error {
		data, err := json.Marshal(rel)
		if err != nil {
			return err
		}
		return s.cache.Put(key, string(data), s.ttl)
	}

	cache, err := s.cache.Get(key)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else {
		if err = json.Unmarshal([]byte(cache), rel); err == nil {
			return rel, nil
		}
	}
	repos, err := s.backend.Repos(owner)
	if err != nil {
		return nil, err
	}
	rel.CommitID = repos[repo]
	if rel.CommitID == "" {
		_ = pushMeta()
		return nil, os.ErrNotExist
	}
	if branch != "" {
		branches, err := s.backend.Branches(owner, repo)
		if err != nil {
			return nil, err
		}
		rel.CommitID = branches[branch]
	}
	if rel.CommitID == "" {
		_ = pushMeta()
		return nil, os.ErrNotExist
	}
	if cname, err := s.ReadString(owner, repo, rel.CommitID, "CNAME"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else {
		rel.Domain = strings.TrimSpace(cname)
	}
	if find, _ := s.FileExists(owner, repo, rel.CommitID, "index.html"); find {
		rel.IsPage = true
	}
	if find, _ := s.FileExists(owner, repo, rel.CommitID, ".history-mode"); find {
		rel.HistoryRouteMode = true
	}
	_ = pushMeta()
	return rel, nil
}

func (s *ServerMeta) ReadString(owner, repo, branch, path string) (string, error) {
	resp, err := s.backend.Open(s.client, owner, repo, branch, path, nil)
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
	resp, err := s.backend.Open(s.client, owner, repo, branch, path, nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	return false, nil
}
