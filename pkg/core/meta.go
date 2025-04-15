package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"code.d7z.net/d7z-project/gitea-pages/pkg/renders"

	"github.com/gobwas/glob"

	"go.uber.org/zap"

	"github.com/pkg/errors"

	"code.d7z.net/d7z-project/gitea-pages/pkg/utils"
)

var regexpHostname = regexp.MustCompile(`^(?:([a-z0-9-]+|\*)\.)?([a-z0-9-]{1,61})\.([a-z0-9]{2,7})$`)

type ServerMeta struct {
	Backend

	client *http.Client
	cache  utils.Config
	ttl    time.Duration

	locker *utils.Locker
}

type renderCompiler struct {
	regex glob.Glob
	renders.Render
}

type PageMetaContent struct {
	CommitID         string            `json:"id"`               // 提交 COMMIT ID
	IsPage           bool              `json:"pg"`               // 是否为 Page
	Domain           string            `json:"domain"`           // 匹配的域名
	HistoryRouteMode bool              `json:"historyRouteMode"` // 路由模式
	CustomNotFound   bool              `json:"404"`              // 注册了自定义 404 页面
	LastModified     time.Time         `json:"up"`               // 上次更新时间
	Renders          map[string]string `json:"renders"`          // 配置的渲染器

	rendersL []*renderCompiler
}

func (m *PageMetaContent) From(data string) error {
	err := json.Unmarshal([]byte(data), m)
	clear(m.rendersL)
	for key, g := range m.Renders {
		m.rendersL = append(m.rendersL, &renderCompiler{
			regex:  glob.MustCompile(g),
			Render: renders.GetRender(key),
		})
	}
	return err
}

func (m *PageMetaContent) String() string {
	marshal, _ := json.Marshal(m)
	return string(marshal)
}

func NewServerMeta(client *http.Client, backend Backend, config utils.Config, ttl time.Duration) *ServerMeta {
	return &ServerMeta{backend, client, config, ttl, utils.NewLocker()}
}

func (s *ServerMeta) GetMeta(baseDomain, owner, repo, branch string) (*PageMetaContent, error) {
	rel := &PageMetaContent{
		IsPage:  false,
		Renders: make(map[string]string),
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
	if find, _ := s.FileExists(owner, repo, rel.CommitID, "404.html"); !find {
		rel.CustomNotFound = find
	}
	if cname, err := s.ReadString(owner, repo, rel.CommitID, "CNAME"); err == nil {
		cname = strings.TrimSpace(cname)
		if regexpHostname.MatchString(cname) && !strings.HasSuffix(strings.ToLower(cname), strings.ToLower(baseDomain)) {
			rel.Domain = cname
		} else {
			zap.L().Debug("指定的 CNAME 不合法", zap.String("cname", cname))
		}
	}
	if r, err := s.ReadString(owner, repo, rel.CommitID, ".render"); err == nil {
		for _, render := range strings.Split(r, "\n") {
			render = strings.TrimSpace(render)
			if strings.HasPrefix(render, "#") {
				continue
			}
			before, after, found := strings.Cut(render, " ")
			before = strings.TrimSpace(before)
			after = strings.TrimSpace(after)
			if found {
				if r := renders.GetRender(before); r != nil {
					if g, err := glob.Compile(after); err == nil {
						rel.Renders[before] = after
						rel.rendersL = append(rel.rendersL, &renderCompiler{
							regex:  g,
							Render: r,
						})
					}
				}
			}

		}
	}
	if find, _ := s.FileExists(owner, repo, rel.CommitID, ".history"); find {
		rel.HistoryRouteMode = true
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
