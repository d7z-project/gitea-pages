package core

import (
	"context"
	"os"
	"strings"

	"github.com/pkg/errors"
	"gopkg.d7z.net/middleware/kv"

	"go.uber.org/zap"
)

type PageDomain struct {
	*ServerMeta

	alias         *DomainAlias
	baseDomain    string
	defaultBranch string
}

func NewPageDomain(meta *ServerMeta, alias kv.KV, baseDomain, defaultBranch string) *PageDomain {
	return &PageDomain{
		baseDomain:    baseDomain,
		defaultBranch: defaultBranch,
		ServerMeta:    meta,
		alias:         NewDomainAlias(alias),
	}
}

type PageDomainContent struct {
	*PageMetaContent

	Owner string
	Repo  string
	Path  string
}

func (p *PageDomain) ParseDomainMeta(ctx context.Context, domain, path, branch string) (*PageDomainContent, error) {
	if branch == "" {
		branch = p.defaultBranch
	}
	pathArr := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if !strings.HasSuffix(domain, "."+p.baseDomain) {
		alias, err := p.alias.Query(ctx, domain) // 确定 alias 是否存在内容
		if err != nil {
			zap.L().Warn("未知域名", zap.String("base", p.baseDomain), zap.String("domain", domain), zap.Error(err))
			return nil, os.ErrNotExist
		}
		zap.L().Debug("命中别名", zap.String("domain", domain), zap.Any("alias", alias))
		return p.ReturnMeta(ctx, alias.Owner, alias.Repo, alias.Branch, pathArr)
	}
	owner := strings.TrimSuffix(domain, "."+p.baseDomain)
	repo := pathArr[0]
	var returnMeta *PageDomainContent
	var err error
	if repo == "" {
		// 回退到默认仓库 (路径未包含仓库)
		zap.L().Debug("fail back to default repo", zap.String("repo", domain))
		returnMeta, err = p.ReturnMeta(ctx, owner, domain, branch, pathArr)
	} else {
		returnMeta, err = p.ReturnMeta(ctx, owner, repo, branch, pathArr[1:])
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err == nil {
		return returnMeta, nil
	}
	// 发现 repo 的情况下回退到默认页面
	return p.ReturnMeta(ctx, owner, domain, branch, pathArr)
}

func (p *PageDomain) ReturnMeta(ctx context.Context, owner string, repo string, branch string, path []string) (*PageDomainContent, error) {
	rel := &PageDomainContent{}
	if meta, err := p.GetMeta(ctx, owner, repo, branch); err == nil {
		rel.PageMetaContent = meta
		rel.Owner = owner
		rel.Repo = repo
		rel.Path = strings.Join(path, "/")
		if err = p.alias.Bind(ctx, meta.Alias, rel.Owner, rel.Repo, branch); err != nil {
			zap.L().Warn("别名绑定失败", zap.Error(err))
			return nil, err
		}
		return rel, nil
	} else {
		zap.L().Debug("查询错误", zap.Error(err))
		if meta != nil {
			// 解析错误汇报
			return nil, errors.New(meta.ErrorMsg)
		}
	}
	return nil, errors.Wrap(os.ErrNotExist, strings.Join(path, "/"))
}
