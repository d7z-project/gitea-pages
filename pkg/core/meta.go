package core

import (
	"context"
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

	"github.com/gobwas/glob"

	"github.com/pkg/errors"

	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

var regexpHostname = regexp.MustCompile(`^(?:([a-z0-9-]+|\*)\.)?([a-z0-9-]{1,61})\.([a-z0-9]{2,7})$`)

type ServerMeta struct {
	Backend
	Domain string

	client *http.Client
	cache  *tools.Cache[PageMetaContent]
	locker *utils.Locker
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

func (s *ServerMeta) GetMeta(ctx context.Context, owner, repo, branch string) (*PageMetaContent, *PageVFS, error) {
	repos, err := s.Repos(ctx, owner)
	if err != nil {
		return nil, nil, err
	}

	defBranch := repos[repo]
	if defBranch == "" {
		return nil, nil, os.ErrNotExist
	}

	if branch == "" {
		branch = defBranch
	}

	branches, err := s.Branches(ctx, owner, repo)
	if err != nil {
		return nil, nil, err
	}

	info := branches[branch]
	if info == nil {
		return nil, nil, os.ErrNotExist
	}

	key := fmt.Sprintf("%s/%s/%s", owner, repo, branch)

	if cache, found := s.cache.Load(ctx, key); found {
		if cache.IsPage {
			return &cache, NewPageVFS(s.client, s.Backend, owner, repo, cache.CommitID), nil
		}
		return nil, nil, os.ErrNotExist
	}

	mux := s.locker.Open(key)
	mux.Lock()
	defer mux.Unlock()

	if cache, found := s.cache.Load(ctx, key); found {
		if cache.IsPage {
			return &cache, NewPageVFS(s.client, s.Backend, owner, repo, cache.CommitID), nil
		}
		return nil, nil, os.ErrNotExist
	}

	rel := NewEmptyPageMetaContent()
	vfs := NewPageVFS(s.client, s.Backend, owner, repo, info.ID)
	rel.CommitID = info.ID
	rel.LastModified = info.LastModified

	// 检查是否存在 index.html
	if exists, _ := vfs.Exists(ctx, "index.html"); !exists {
		rel.IsPage = false
		_ = s.cache.Store(ctx, key, *rel)
		return nil, nil, os.ErrNotExist
	}

	rel.IsPage = true

	// 添加默认跳过的内容
	for _, defIgnore := range rel.Ignore {
		rel.ignoreL = append(rel.ignoreL, glob.MustCompile(defIgnore))
	}

	// 解析配置
	if err := s.parsePageConfig(ctx, rel, vfs); err != nil {
		rel.IsPage = false
		rel.ErrorMsg = err.Error()
		_ = s.cache.Store(ctx, key, *rel)
		return nil, nil, err
	}

	// 处理 CNAME 文件
	if err := s.parseCNAME(ctx, rel, vfs); err != nil {
		rel.IsPage = false
		rel.ErrorMsg = err.Error()
		_ = s.cache.Store(ctx, key, *rel)
		return nil, nil, err
	}

	rel.Alias = utils.ClearDuplicates(rel.Alias)
	rel.Ignore = utils.ClearDuplicates(rel.Ignore)
	_ = s.cache.Store(ctx, key, *rel)
	return rel, vfs, nil
}

func (s *ServerMeta) parsePageConfig(ctx context.Context, rel *PageMetaContent, vfs *PageVFS) error {
	data, err := vfs.ReadString(ctx, ".pages.yaml")
	if err != nil {
		zap.L().Debug("failed to read meta data", zap.String("error", err.Error()))
		return nil // 配置文件不存在不是错误
	}

	cfg := new(PageConfig)
	if err = yaml.Unmarshal([]byte(data), cfg); err != nil {
		return errors.Wrap(err, "parse .pages.yaml failed")
	}

	rel.VRoute = cfg.VirtualRoute

	// 处理别名
	for _, cname := range cfg.Alias {
		if err := s.addAlias(rel, cname); err != nil {
			return err
		}
	}

	// 处理渲染器
	for sType, pattern := range cfg.Renders() {
		r := GetRender(sType)
		if r == nil {
			return errors.Errorf("render not found %s", sType)
		}

		g, err := glob.Compile(strings.TrimSpace(pattern))
		if err != nil {
			return errors.Wrapf(err, "compile render pattern failed: %s", pattern)
		}

		rel.rendersL = append(rel.rendersL, &renderCompiler{
			regex:  g,
			Render: r,
		})
		rel.Renders[sType] = append(rel.Renders[sType], pattern)
	}

	// 处理跳过内容
	for _, pattern := range cfg.Ignores() {
		g, err := glob.Compile(pattern)
		if err != nil {
			return errors.Wrapf(err, "compile ignore pattern failed: %s", pattern)
		}
		rel.ignoreL = append(rel.ignoreL, g)
		rel.Ignore = append(rel.Ignore, pattern)
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

		rel.Proxy[path] = rURL.String()
	}

	return nil
}

func (s *ServerMeta) parseCNAME(ctx context.Context, rel *PageMetaContent, vfs *PageVFS) error {
	cname, err := vfs.ReadString(ctx, "CNAME")
	if err != nil {
		return nil // CNAME 文件不存在是正常情况
	}
	if err := s.addAlias(rel, cname); err != nil {
		zap.L().Debug("指定的 CNAME 不合法", zap.String("cname", cname), zap.Error(err))
		return err
	}
	return nil
}

func (s *ServerMeta) addAlias(rel *PageMetaContent, cname string) error {
	cname = strings.TrimSpace(cname)
	if !regexpHostname.MatchString(cname) {
		return errors.New("invalid domain name format")
	}

	if strings.HasSuffix(strings.ToLower(cname), strings.ToLower(s.Domain)) {
		return errors.New("alias cannot be subdomain of main domain")
	}

	rel.Alias = append(rel.Alias, cname)
	return nil
}
