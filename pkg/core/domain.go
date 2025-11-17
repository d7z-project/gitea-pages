package core

import (
	"context"
	"os"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type PageDomain struct {
	*ServerMeta

	alias         *DomainAlias
	baseDomain    string
	defaultBranch string
}

func NewPageDomain(meta *ServerMeta, alias *DomainAlias, baseDomain, defaultBranch string) *PageDomain {
	return &PageDomain{
		baseDomain:    baseDomain,
		defaultBranch: defaultBranch,
		ServerMeta:    meta,
		alias:         alias,
	}
}

type PageContent struct {
	*PageMetaContent

	Owner string
	Repo  string
	Path  string
}

func (p *PageDomain) ParseDomainMeta(ctx context.Context, domain, path, branch string) (*PageContent, error) {
	if branch == "" {
		branch = p.defaultBranch
	}
	pathArr := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if !strings.HasSuffix(domain, "."+p.baseDomain) {
		alias, err := p.alias.Query(ctx, domain) // 确定 alias 是否存在内容
		if err != nil {
			zap.L().Warn("unknown domain", zap.String("base", p.baseDomain), zap.String("domain", domain), zap.Error(err))
			return nil, os.ErrNotExist
		}
		zap.L().Debug("alias hit", zap.String("domain", domain), zap.Any("alias", alias))
		return p.returnMeta(ctx, alias.Owner, alias.Repo, alias.Branch, pathArr)
	}
	owner := strings.TrimSuffix(domain, "."+p.baseDomain)
	repo := pathArr[0]
	var returnMeta *PageContent
	var err error
	if repo == "" {
		// 回退到默认仓库 (路径未包含仓库)
		zap.L().Debug("fail back to default repo", zap.String("repo", domain))
		returnMeta, err = p.returnMeta(ctx, owner, domain, branch, pathArr)
	} else {
		returnMeta, err = p.returnMeta(ctx, owner, repo, branch, pathArr[1:])
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err == nil {
		return returnMeta, nil
	}
	// 发现 repo 的情况下回退到默认页面
	return p.returnMeta(ctx, owner, domain, branch, pathArr)
}

func (p *PageDomain) returnMeta(ctx context.Context, owner, repo, branch string, path []string) (*PageContent, error) {
	result := &PageContent{}
	meta, err := p.GetMeta(ctx, owner, repo, branch)
	if err != nil {
		zap.L().Debug("repo does not exists", zap.Error(err), zap.Strings("meta", []string{owner, repo, branch}))
		if meta != nil {
			// 解析错误汇报
			return nil, errors.New(meta.ErrorMsg)
		}
		return nil, errors.Wrap(os.ErrNotExist, strings.Join(path, "/"))
	}
	result.PageMetaContent = meta
	result.Owner = owner
	result.Repo = repo
	result.Path = strings.Join(path, "/")

	if err = p.alias.Bind(ctx, meta.Alias, result.Owner, result.Repo, branch); err != nil {
		zap.L().Warn("alias binding error.", zap.Error(err))
		return nil, err
	}
	return result, nil
}
