package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gobwas/glob"
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
	cache  *tools.KVCache[PageMetaContent]
	locker *utils.Locker
}

// PageConfig 配置

type PageMetaContent struct {
	CommitID     string    `json:"commit_id"`     // 提交 COMMIT ID
	LastModified time.Time `json:"last_modified"` // 上次更新时间
	IsPage       bool      `json:"is_page"`       // 是否为 Page
	ErrorMsg     string    `json:"error"`         // 错误消息 (作为 500 错误日志暴露至前端)

	Alias   []string `json:"alias"`   // alias
	Filters []Filter `json:"filters"` // 路由消息
}

func NewEmptyPageMetaContent() *PageMetaContent {
	return &PageMetaContent{
		IsPage: false,
		Filters: []Filter{
			{
				Path:   "**",
				Type:   "404",
				Params: map[string]any{},
			},
			{ // 默认阻塞
				Path:   ".git/**",
				Type:   "block",
				Params: map[string]any{},
			},
			{ // 默认阻塞
				Path:   ".pages.yaml",
				Type:   "block",
				Params: map[string]any{},
			},
		},
	}
}

func (m *PageMetaContent) String() string {
	marshal, _ := json.Marshal(m)
	return string(marshal)
}

func NewServerMeta(client *http.Client, backend Backend, domain string, cache kv.KV, ttl time.Duration) *ServerMeta {
	return &ServerMeta{
		Backend: backend,
		Domain:  domain,
		client:  client,
		cache:   tools.NewCache[PageMetaContent](cache, "meta", ttl),
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
	vfs := NewPageVFS(s.Backend, owner, repo, info.ID)
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
	defer func() {
		meta.Filters = append(meta.Filters, Filter{
			Path: "**",
			Type: "direct",
			Params: map[string]any{
				"prefix": "",
			},
		})
	}()
	alias := make([]string, 0)
	cname, err := vfs.ReadString(ctx, "CNAME")
	if cname != "" && err == nil {
		cname = strings.TrimSpace(cname)
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

	// 处理别名
	for _, item := range cfg.Alias {
		if item == "" {
			continue
		}
		if al, ok := s.aliasCheck(item); ok {
			alias = append(alias, al)
		} else {
			return fmt.Errorf("invalid alias %s", item)
		}
	}
	if len(alias) > 0 {
		meta.Filters = append(meta.Filters, Filter{
			Path: "**",
			Type: "redirect",
			Params: map[string]any{
				"targets": alias,
			},
		})
	}
	meta.Alias = alias
	// 处理自定义路由
	for _, r := range cfg.Routes {
		for _, item := range strings.Split(r.Path, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, err := glob.Compile(item); err != nil {
				return errors.Wrapf(err, "invalid route glob pattern: %s", item)
			}
			meta.Filters = append(meta.Filters, Filter{
				Path:   item,
				Type:   r.Type,
				Params: r.Params,
			})
		}
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
