package core

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.d7z.net/gitea-pages/pkg/utils"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var portExp = regexp.MustCompile(`:\d+$`)

type PageDomain struct {
	*ServerMeta

	alias         *DomainAlias
	baseDomain    string
	defaultBranch string
}

func NewPageDomain(meta *ServerMeta, config utils.Config, baseDomain, defaultBranch string) *PageDomain {
	return &PageDomain{
		baseDomain:    baseDomain,
		defaultBranch: defaultBranch,
		ServerMeta:    meta,
		alias:         NewDomainAlias(config),
	}
}

type PageDomainContent struct {
	*PageMetaContent

	Owner string
	Repo  string
	Path  string
}

func (m *PageDomainContent) CacheKey() string {
	return fmt.Sprintf("%s/%s/%s/%s", m.Owner, m.Repo, m.CommitID, m.Path)
}

func (p *PageDomain) ParseDomainMeta(domain, path, branch string) (*PageDomainContent, error) {
	if branch == "" {
		branch = p.defaultBranch
	}
	domain = portExp.ReplaceAllString(strings.ToLower(domain), "")
	pathS := strings.Split(strings.TrimPrefix(path, "/"), "/")

	if !strings.HasSuffix(domain, "."+p.baseDomain) {
		alias, err := p.alias.Query(domain)
		if err != nil {
			zap.L().Warn("未知域名", zap.String("base", p.baseDomain), zap.String("domain", domain), zap.Error(err))
			return nil, os.ErrNotExist
		}
		zap.L().Debug("命中别名", zap.String("domain", domain), zap.Any("alias", alias))
		return p.ReturnMeta(alias.Owner, alias.Repo, alias.Branch, pathS)
	}
	owner := strings.TrimSuffix(domain, "."+p.baseDomain)
	repo := pathS[0]
	if repo == "" {
		// 回退到默认仓库
		repo = p.baseDomain
		zap.L().Debug("fail back to default repo", zap.String("repo", repo))
	}
	returnMeta, err := p.ReturnMeta(owner, repo, branch, pathS[1:])
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err == nil {
		return returnMeta, nil
	}
	// 回退到默认页面
	return p.ReturnMeta(owner, repo, domain, pathS)
}

func (p *PageDomain) ReturnMeta(owner string, repo string, branch string, path []string) (*PageDomainContent, error) {
	rel := &PageDomainContent{}
	if meta, err := p.GetMeta(p.baseDomain, owner, repo, branch); err == nil {
		rel.PageMetaContent = meta
		rel.Owner = owner
		rel.Repo = repo
		rel.Path = strings.Join(path, "/")
		if strings.HasSuffix(rel.Path, "/") || rel.Path == "" {
			rel.Path = rel.Path + "index.html"
		}
		if meta.Domain != "" {
			err = p.alias.Bind(meta.Domain, rel.Owner, rel.Repo, branch)
			if err != nil {
				zap.L().Warn("别名绑定失败", zap.Error(err))
			}
		}

		return rel, nil
	} else {
		zap.L().Debug("查询错误", zap.Error(err))
	}
	return nil, errors.Wrapf(os.ErrNotExist, strings.Join(path, "/"))
}
