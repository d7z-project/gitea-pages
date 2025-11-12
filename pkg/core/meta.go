package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/tools"
	"gopkg.in/yaml.v3"

	"github.com/pkg/errors"

	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

type ServerMeta struct {
	Backend
	Domain string

	client *http.Client
	cache  *tools.Cache[PageMetaContent]
	locker *utils.Locker
}

// PageConfig 配置

type PageMetaContent struct {
	CommitID     string    `json:"commit_id"`     // 提交 COMMIT ID
	LastModified time.Time `json:"last_modified"` // 上次更新时间
	IsPage       bool      `json:"is_page"`       // 是否为 Page
	ErrorMsg     string    `json:"error"`         // 错误消息 (作为 500 错误日志暴露至前端)

	Alias []string `json:"alias"` // alias

	Filters []Filter `json:"filters"` // 路由消息
}

func NewEmptyPageMetaContent() *PageMetaContent {
	return &PageMetaContent{
		IsPage: false,
		Filters: []Filter{
			{
				Path:   "**",
				Type:   "default_not_found",
				Params: map[string]any{},
			},
			{ // 默认阻塞
				Path: ".git/**",
				Type: "block",
				Params: map[string]any{
					"code":    "404",
					"message": "Not found",
				},
			}, { // 默认阻塞
				Path: ".pages.yaml",
				Type: "block",
				Params: map[string]any{
					"code":    "404",
					"message": "Not found",
				},
			},
		},
	}
}

func (m *PageMetaContent) String() string {
	marshal, _ := json.Marshal(m)
	return string(marshal)
}

func NewServerMeta(client *http.Client, backend Backend, kv kv.KV, domain string, ttl time.Duration) *ServerMeta {
	return &ServerMeta{
		Backend: backend,
		Domain:  domain,
		client:  client,
		cache:   tools.NewCache[PageMetaContent](kv, "pages/meta", ttl),
		locker:  utils.NewLocker(),
	}
}

func (s *ServerMeta) GetMeta(ctx context.Context, owner, repo, branch string) (*PageMetaContent, error) {
	repos, err := s.Repos(ctx, owner)
	if err != nil {
		return nil, err
	}

	defBranch := repos[repo]
	if defBranch == "" {
		return nil, os.ErrNotExist
	}

	if branch == "" {
		branch = defBranch
	}

	branches, err := s.Branches(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	info := branches[branch]
	if info == nil {
		return nil, os.ErrNotExist
	}

	key := fmt.Sprintf("%s/%s/%s", owner, repo, branch)

	if cache, found := s.cache.Load(ctx, key); found {
		if cache.IsPage {
			return &cache, nil
		}
		return nil, os.ErrNotExist
	}

	mux := s.locker.Open(key)
	mux.Lock()
	defer mux.Unlock()

	if cache, found := s.cache.Load(ctx, key); found {
		if cache.IsPage {
			return &cache, nil
		}
		return nil, os.ErrNotExist
	}

	rel := NewEmptyPageMetaContent()
	vfs := NewPageVFS(s.client, s.Backend, owner, repo, info.ID)
	rel.CommitID = info.ID
	rel.LastModified = info.LastModified

	// 检查是否存在 index.html
	if exists, _ := vfs.Exists(ctx, "index.html"); !exists {
		rel.IsPage = false
		_ = s.cache.Store(ctx, key, *rel)
		return nil, os.ErrNotExist
	}
	rel.IsPage = true
	// 解析配置
	if err := s.parsePageConfig(ctx, rel, vfs); err != nil {
		rel.IsPage = false
		rel.ErrorMsg = err.Error()
		_ = s.cache.Store(ctx, key, *rel)
		return nil, err
	}

	_ = s.cache.Store(ctx, key, *rel)
	return rel, nil
}

func (s *ServerMeta) parsePageConfig(ctx context.Context, meta *PageMetaContent, vfs *PageVFS) error {
	alias := make([]string, 0)
	defer func(alias *[]string) {
		meta.Alias = *alias
		direct := *alias
		meta.Filters = append(meta.Filters, Filter{
			Path: "**",
			Type: "redirect",
			Params: map[string]any{
				"targets": direct,
			},
		})
	}(&alias)
	cname, err := vfs.ReadString(ctx, "CNAME")
	if cname != "" && err == nil {
		if al, ok := s.aliasCheck(cname); ok {
			alias = append(alias, al)
		} else {
			return fmt.Errorf("invalid alias %s", cname)
		}
	}
	data, err := vfs.ReadString(ctx, ".pages.yaml")
	if err != nil {
		zap.L().Debug("failed to read meta data", zap.String("error", err.Error()))
		return nil // 配置文件不存在不是错误
	}

	cfg := new(PageConfig)
	if err = yaml.Unmarshal([]byte(data), cfg); err != nil {
		return errors.Wrap(err, "parse .pages.yaml failed")
	}
	if cfg.VirtualRoute {
		meta.Filters = append(meta.Filters, Filter{
			Path: "**",
			Type: "forward",
			Params: map[string]any{
				"path": "index.html",
			},
		})
	}

	// 处理别名
	for _, cname := range cfg.Alias {
		if cname == "" {
			continue
		}
		if al, ok := s.aliasCheck(cname); ok {
			alias = append(alias, al)
		} else {
			return fmt.Errorf("invalid alias %s", cname)
		}
	}

	// 处理渲染器
	for sType, pattern := range cfg.Renders() {
		meta.Filters = append(meta.Filters, Filter{
			Path:   pattern,
			Type:   sType,
			Params: map[string]any{},
		})
	}

	// 处理跳过内容
	for _, pattern := range cfg.Ignores() {
		meta.Filters = append(meta.Filters, Filter{ // 默认直连
			Path: pattern,
			Type: "block",
			Params: map[string]any{
				"code":    "404",
				"message": "Not found",
			},
		},
		)
	}

	// 处理反向代理
	for path, backend := range cfg.ReverseProxy {
		path = filepath.ToSlash(filepath.Clean(path))
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		path = strings.TrimSuffix(path, "/")

		rURL, err := url.Parse(backend)
		if err != nil {
			return errors.Wrapf(err, "parse backend url failed: %s", backend)
		}

		if rURL.Scheme != "http" && rURL.Scheme != "https" {
			return errors.Errorf("invalid backend url scheme: %s", backend)
		}
		meta.Filters = append(meta.Filters, Filter{
			Path: path,
			Type: "reverse_proxy",
			Params: map[string]any{
				"prefix": path,
				"target": rURL.String(),
			},
		})
	}

	return nil
}

var regexpHostname = regexp.MustCompile(`^(?:([a-z0-9-]+|\*)\.)?([a-z0-9-]{1,61})\.([a-z0-9]{2,7})$`)

func (s *ServerMeta) aliasCheck(cname string) (string, bool) {
	cname = strings.TrimSpace(cname)
	if !regexpHostname.MatchString(cname) {
		return "", false
	}

	if strings.HasSuffix(strings.ToLower(cname), strings.ToLower(s.Domain)) {
		return "", false
	}
	return cname, true
}
