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
	Alias  *DomainAlias

	client     *http.Client
	cache      *tools.KVCache[PageMetaContent]
	locker     *utils.Locker
	refresh    time.Duration
	refreshSem chan struct{}
}

// PageConfig 配置

type PageMetaContent struct {
	CommitID     string    `json:"commit_id"`     // 提交 COMMIT ID
	LastModified time.Time `json:"last_modified"` // 上次更新时间
	IsPage       bool      `json:"is_page"`       // 是否为 Page
	ErrorMsg     string    `json:"error"`         // 错误消息 (作为 500 错误日志暴露至前端)
	RefreshAt    time.Time `json:"refresh_at"`    // 下次刷新时间

	Alias   []string `json:"alias"`   // alias
	Filters []Filter `json:"filters"` // 路由消息
}

func NewEmptyPageMetaContent() *PageMetaContent {
	return &PageMetaContent{
		IsPage:    false,
		RefreshAt: time.Now(),
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

func NewServerMeta(
	client *http.Client,
	backend Backend,
	domain string,
	alias *DomainAlias,
	cache kv.KV,
	ttl time.Duration,
	refresh time.Duration,
	refreshConcurrent int,
) *ServerMeta {
	if refreshConcurrent <= 0 {
		refreshConcurrent = 16
	}
	return &ServerMeta{
		Backend:    backend,
		Domain:     domain,
		Alias:      alias,
		client:     client,
		cache:      tools.NewCache[PageMetaContent](cache, "meta", ttl),
		locker:     utils.NewLocker(),
		refresh:    refresh,
		refreshSem: make(chan struct{}, refreshConcurrent),
	}
}

func (s *ServerMeta) GetMeta(ctx context.Context, owner, repo string) (*PageMetaContent, error) {
	key := fmt.Sprintf("%s/%s", owner, repo)
	if cache, found := s.cache.Load(ctx, key); found {
		if time.Now().After(cache.RefreshAt) {
			// 异步刷新
			mux := s.locker.Open(key)
			if mux.TryLock() {
				select {
				case s.refreshSem <- struct{}{}:
					go func() {
						defer func() { <-s.refreshSem }()
						defer mux.Unlock()
						bgCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
						defer cancel()
						_, _ = s.updateMetaWithLock(bgCtx, owner, repo)
					}()
				default:
					// 达到并发限制，跳过本次异步刷新，直接返回旧缓存
					mux.Unlock()
				}
			}
		}
		if cache.IsPage {
			return &cache, nil
		}
		return nil, os.ErrNotExist
	}
	return s.updateMeta(ctx, owner, repo)
}

func (s *ServerMeta) updateMeta(ctx context.Context, owner, repo string) (*PageMetaContent, error) {
	key := fmt.Sprintf("%s/%s", owner, repo)
	mux := s.locker.Open(key)
	mux.Lock()
	defer mux.Unlock()

	return s.updateMetaWithLock(ctx, owner, repo)
}

func (s *ServerMeta) updateMetaWithLock(ctx context.Context, owner, repo string) (*PageMetaContent, error) {
	key := fmt.Sprintf("%s/%s", owner, repo)
	// 再次检查缓存
	if cache, found := s.cache.Load(ctx, key); found && time.Now().Before(cache.RefreshAt) {
		if cache.IsPage {
			return &cache, nil
		}
		return nil, os.ErrNotExist
	}

	rel := NewEmptyPageMetaContent()
	info, err := s.Meta(ctx, owner, repo)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			rel.IsPage = false
			rel.RefreshAt = time.Now().Add(s.refresh)
			_ = s.cache.Store(ctx, key, *rel)
		}
		return nil, err
	}
	vfs := NewPageVFS(s.Backend, owner, repo, info.ID)
	rel.CommitID = info.ID
	rel.LastModified = info.LastModified
	rel.RefreshAt = time.Now().Add(s.refresh)

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
	// todo: 优化保存逻辑 ，减少写入
	if err = s.Alias.Bind(ctx, rel.Alias, owner, repo); err != nil {
		zap.L().Warn("alias binding error.", zap.Error(err))
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
		if len(alias) > 0 {
			meta.Filters = append(meta.Filters, Filter{
				Path: "**",
				Type: "redirect",
				Params: map[string]any{
					"targets": alias,
				},
			})
			meta.Alias = alias
		}
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
