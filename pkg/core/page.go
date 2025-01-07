package core

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type PageDomain struct {
	*ServerMeta

	baseDomain    string
	defaultBranch string
}

func NewPageDomain(meta *ServerMeta, baseDomain, defaultBranch string) *PageDomain {
	return &PageDomain{
		baseDomain:    baseDomain,
		defaultBranch: defaultBranch,
		ServerMeta:    meta,
	}
}

type PageDomainContent struct {
	*PageMetaContent

	Owner string
	Repo  string
	Path  string
}

func (m *PageDomainContent) CacheKey() string {
	return fmt.Sprintf("%s/%s/%s%s", m.Owner, m.Repo, m.CommitID, m.Path)
}

func (p *PageDomain) ParseDomainMeta(domain, path, branch string) (*PageDomainContent, error) {
	if branch == "" {
		branch = p.defaultBranch
	}

	rel := &PageDomainContent{}
	if !strings.HasSuffix(domain, "."+p.baseDomain) {
		return nil, os.ErrNotExist
	}

	rel.Owner = strings.TrimSuffix(domain, "."+p.baseDomain)
	pathS := strings.Split(strings.TrimPrefix(path, "/"), "/")
	repo := pathS[0]
	defaultRepo := rel.Owner + "." + p.baseDomain
	if repo == "" {
		// 回退到默认仓库
		rel.Repo = defaultRepo
	}

	meta, err := p.GetMeta(rel.Owner, rel.Repo, branch)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		rel.Path = "/" + strings.Join(pathS[1:], "/")
		rel.PageMetaContent = meta
		return rel, nil
	}
	if defaultRepo == rel.Repo {
		return nil, os.ErrNotExist
	}
	if meta, err := p.GetMeta(rel.Owner, defaultRepo, branch); err == nil {
		rel.PageMetaContent = meta
		rel.Repo = defaultRepo
		rel.Path = "/" + strings.Join(pathS, "/")
		return rel, nil
	}
	if strings.HasSuffix(path, "/") {
		rel.Path = rel.Path + "/index.html"
	}
	return nil, os.ErrNotExist
}
