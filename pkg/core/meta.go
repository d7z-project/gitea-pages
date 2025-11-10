package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	"gopkg.d7z.net/middleware/kv"
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
	
	cache kv.KV
	ttl   time.Duration

	locker *utils.Locker
}

func NewServerMeta(client *http.Client, backend Backend, kv kv.KV, domain string, ttl time.Duration) *ServerMeta {
	return &ServerMeta{backend, domain, client, kv, ttl, utils.NewLocker()}
}

func (s *ServerMeta) GetMeta(ctx context.Context, owner, repo, branch string) (*PageMetaContent, error) {
	rel := NewPageMetaContent()
	if repos, err := s.Repos(ctx, owner); err != nil {
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
	if branches, err := s.Branches(ctx, owner, repo); err != nil {
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
	cache, err := s.cache.Get(ctx, key)
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
	cache, err = s.cache.Get(ctx, key)
	if err == nil {
		if err = rel.From(cache); err == nil {
			if !rel.IsPage {
				return nil, os.ErrNotExist
			}
			return rel, nil
		}
	}

	// 确定存在 index.html , 否则跳过
	if find, _ := s.FileExists(ctx, owner, repo, rel.CommitID, "index.html"); !find {
		rel.IsPage = false
		_ = s.cache.Put(ctx, key, rel.String(), s.ttl)
		return nil, os.ErrNotExist
	} else {
		rel.IsPage = true
	}
	errFunc := func(err error) (*PageMetaContent, error) {
		rel.IsPage = false
		rel.ErrorMsg = err.Error()
		_ = s.cache.Put(ctx, key, rel.String(), s.ttl)
		return nil, err
	}
	// 添加默认跳过的内容
	for _, defIgnore := range rel.Ignore {
		rel.ignoreL = append(rel.ignoreL, glob.MustCompile(defIgnore))
	}
	// 解析配置
	if data, err := s.ReadString(ctx, owner, repo, rel.CommitID, ".pages.yaml"); err == nil {
		cfg := new(PageConfig)
		if err = yaml.Unmarshal([]byte(data), cfg); err != nil {
			return errFunc(err)
		}
		rel.VRoute = cfg.VirtualRoute
		// 处理 CNAME
		for _, cname := range cfg.Alias {
			cname = strings.TrimSpace(cname)
			if regexpHostname.MatchString(cname) && !strings.HasSuffix(strings.ToLower(cname), strings.ToLower(s.Domain)) {
				rel.Alias = append(rel.Alias, cname)
			} else {
				return errFunc(errors.New("invalid alias name " + cname))
			}
		}
		// 处理渲染器
		for sType, pattern := range cfg.Renders() {
			var r Render
			if r = GetRender(sType); r == nil {
				return errFunc(errors.Errorf("render not found %s", sType))
			}
			if g, err := glob.Compile(strings.TrimSpace(pattern)); err == nil {
				rel.rendersL = append(rel.rendersL, &renderCompiler{
					regex:  g,
					Render: r,
				})
			} else {
				return errFunc(err)
			}
			rel.Renders[sType] = append(rel.Renders[sType], pattern)
		}
		// 处理跳过内容
		for _, pattern := range cfg.Ignores() {
			if g, err := glob.Compile(pattern); err == nil {
				rel.ignoreL = append(rel.ignoreL, g)
			} else {
				return errFunc(err)
			}
			rel.Ignore = append(rel.Ignore, pattern)
		}
		// 处理反向代理 (清理内容，符合 /<item>)
		for path, backend := range cfg.ReverseProxy {
			path = filepath.ToSlash(filepath.Clean(path))
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

	// 兼容 github 的 CNAME 模式
	if cname, err := s.ReadString(ctx, owner, repo, rel.CommitID, "CNAME"); err == nil {
		cname = strings.TrimSpace(cname)
		if regexpHostname.MatchString(cname) && !strings.HasSuffix(strings.ToLower(cname), strings.ToLower(s.Domain)) {
			rel.Alias = append(rel.Alias, cname)
		} else {
			zap.L().Debug("指定的 CNAME 不合法", zap.String("cname", cname))
		}
	}
	rel.Alias = utils.ClearDuplicates(rel.Alias)
	rel.Ignore = utils.ClearDuplicates(rel.Ignore)
	_ = s.cache.Put(ctx, key, rel.String(), s.ttl)
	return rel, nil
}

func (s *ServerMeta) ReadString(ctx context.Context, owner, repo, branch, path string) (string, error) {
	resp, err := s.Open(ctx, s.client, owner, repo, branch, path, nil)
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

func (s *ServerMeta) FileExists(ctx context.Context, owner, repo, branch, path string) (bool, error) {
	resp, err := s.Open(ctx, s.client, owner, repo, branch, path, nil)
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
