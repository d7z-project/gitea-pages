package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/gobwas/glob"

	"github.com/pkg/errors"

	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

var regexpHostname = regexp.MustCompile(`^(?:([a-z0-9-]+|\*)\.)?([a-z0-9-]{1,61})\.([a-z0-9]{2,7})$`)

type ServerMeta struct {
	Backend

	Domain string

	client *http.Client
	cache  utils.KVConfig
	ttl    time.Duration

	locker *utils.Locker
}

type renderCompiler struct {
	regex glob.Glob
	Render
}

// PageConfig 配置
type PageConfig struct {
	Alias   []string          `yaml:"required"`  // 重定向地址
	Renders map[string]string `yaml:"templates"` // 渲染器地址

	VirtualRoute bool              `yaml:"v-route"` // 是否使用虚拟路由（任何路径均使用 /index.html 返回 200 响应）
	ReverseProxy map[string]string `yaml:"proxy"`   // 反向代理路由
}

type PageMetaContent struct {
	CommitID     string    `json:"commit-id"`     // 提交 COMMIT ID
	LastModified time.Time `json:"last-modified"` // 上次更新时间
	IsPage       bool      `json:"is-page"`       // 是否为 Page
	ErrorMsg     string    `json:"error"`         // 错误消息

	VRoute  bool                `yaml:"v-route"` // 虚拟路由
	Alias   []string            `yaml:"aliases"` // 重定向
	Proxy   map[string]string   `yaml:"proxy"`   // 反向代理
	Renders map[string][]string `json:"renders"` // 配置的渲染器

	rendersL []*renderCompiler
}

func (m *PageMetaContent) From(data string) error {
	err := json.Unmarshal([]byte(data), m)
	clear(m.rendersL)
	for key, gs := range m.Renders {
		for _, g := range gs {
			m.rendersL = append(m.rendersL, &renderCompiler{
				regex:  glob.MustCompile(g),
				Render: GetRender(key),
			})
		}
	}
	return err
}

func (m *PageMetaContent) TryRender(path ...string) Render {
	for _, s := range path {
		for _, compiler := range m.rendersL {
			if compiler.regex.Match(s) {
				return compiler.Render
			}
		}
	}
	return nil
}

func (m *PageMetaContent) String() string {
	marshal, _ := json.Marshal(m)
	return string(marshal)
}

func NewServerMeta(client *http.Client, backend Backend, kv utils.KVConfig, domain string, ttl time.Duration) *ServerMeta {
	return &ServerMeta{backend, domain, client, kv, ttl, utils.NewLocker()}
}

func (s *ServerMeta) GetMeta(owner, repo, branch string) (*PageMetaContent, error) {
	rel := &PageMetaContent{
		IsPage:  false,
		Proxy:   make(map[string]string),
		Alias:   make([]string, 0),
		Renders: make(map[string][]string),
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
	} else {
		rel.IsPage = true
	}
	errFunc := func(err error) (*PageMetaContent, error) {
		rel.IsPage = false
		rel.ErrorMsg = err.Error()
		_ = s.cache.Put(key, rel.String(), s.ttl)
		return nil, err
	}

	if data, err := s.ReadString(owner, repo, rel.CommitID, ".pages.yaml"); err == nil {
		cfg := new(PageConfig)
		if err = yaml.Unmarshal([]byte(data), cfg); err != nil {
			return errFunc(err)
		}
		rel.Alias = cfg.Alias
		rel.VRoute = cfg.VirtualRoute
		for _, cname := range cfg.Alias {
			cname = strings.TrimSpace(cname)
			if regexpHostname.MatchString(cname) && !strings.HasSuffix(strings.ToLower(cname), strings.ToLower(s.Domain)) {
				rel.Alias = append(rel.Alias, cname)
			} else {
				return errFunc(errors.New("invalid alias name " + cname))
			}
		}
		for sType, patterns := range cfg.Renders {
			if r := GetRender(sType); r != nil {
				for _, pattern := range strings.Split(patterns, ",") {
					rel.Renders[sType] = append(rel.Renders[sType], pattern)
					if g, err := glob.Compile(strings.TrimSpace(pattern)); err == nil {
						rel.rendersL = append(rel.rendersL, &renderCompiler{
							regex:  g,
							Render: r,
						})
					} else {
						return errFunc(err)
					}
				}
			}
		}
		for path, backend := range cfg.ReverseProxy {
			path = strings.TrimSpace(path)
			path = strings.ReplaceAll(path, "//", "/")
			path = strings.ReplaceAll(path, "//", "/")
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			if strings.HasSuffix(path, "/") {
				path = path[:len(path)-1]
			}
			var rUrl *url.URL
			if rUrl, err = url.Parse(backend); err != nil {
				return errFunc(err)
			}
			if rUrl.Scheme != "http" && rUrl.Scheme != "https" {
				return errFunc(errors.New("invalid backend url " + backend))
			}
			rel.Proxy[path] = rUrl.String()
		}
	} else {
		// 不存在配置，但也可以重定向
		zap.L().Debug("failed to read meta data", zap.String("error", err.Error()))
	}

	_ = s.cache.Put(key, rel.String(), s.ttl)
	return rel, nil
}

func (s *ServerMeta) ReadString(owner, repo, branch, path string) (string, error) {
	resp, err := s.Open(s.client, owner, repo, branch, path, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil {
		return "", err
	}
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
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil {
		return false, err
	}
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	return false, nil
}
