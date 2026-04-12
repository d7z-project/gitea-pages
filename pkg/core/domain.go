package core

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/pkg/errors"
)

type PageDomain struct {
	*ServerMeta

	baseDomain string
}

func NewPageDomain(meta *ServerMeta, baseDomain string) *PageDomain {
	return &PageDomain{
		baseDomain: baseDomain,
		ServerMeta: meta,
	}
}

type PageContent struct {
	*PageMetaContent

	Owner string
	Repo  string
	Path  string
}

func (p *PageDomain) ParseDomainMeta(ctx context.Context, domain, path string) (*PageContent, error) {
	pathArr := strings.Split(strings.TrimPrefix(path, "/"), "/")
	defaultRepo := domain
	if !strings.HasSuffix(domain, "."+p.baseDomain) {
		alias, err := p.Alias.Query(ctx, domain) // 确定 alias 是否存在内容
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			slog.Warn("unknown domain", "base", p.baseDomain, "domain", domain, "error", err)
			return nil, os.ErrNotExist
		}
		slog.Debug("alias hit", "domain", domain, "alias", alias)
		return p.returnMeta(ctx, alias.Owner, alias.Repo, pathArr)
	}
	owner := strings.TrimSuffix(domain, "."+p.baseDomain)
	repo := pathArr[0]
	var returnMeta *PageContent
	var err error
	if repo == "" {
		// 回退到默认仓库 (路径未包含仓库)
		slog.Debug("fail back to default repo", "repo", domain)
		returnMeta, err = p.returnMeta(ctx, owner, defaultRepo, pathArr)
	} else {
		returnMeta, err = p.returnMeta(ctx, owner, repo, pathArr[1:])
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err == nil {
		return returnMeta, nil
	}
	// 发现 repo 的情况下回退到默认页面
	return p.returnMeta(ctx, owner, defaultRepo, pathArr)
}

func (p *PageDomain) returnMeta(ctx context.Context, owner, repo string, path []string) (*PageContent, error) {
	result := &PageContent{}
	meta, err := p.GetMeta(ctx, owner, repo)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		slog.Debug("repo does not exists", "error", err, "meta", []string{owner, repo})
		return nil, errors.Wrap(os.ErrNotExist, strings.Join(path, "/"))
	}
	result.PageMetaContent = meta
	result.Owner = owner
	result.Repo = repo
	result.Path = strings.Join(path, "/")

	return result, nil
}
