package core

import (
	"context"
	"encoding/json"
	"fmt"

	"gopkg.d7z.net/gitea-pages/pkg/middleware/config"
)

type Alias struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

type DomainAlias struct {
	config config.KVConfig
}

func NewDomainAlias(config config.KVConfig) *DomainAlias {
	return &DomainAlias{config: config}
}

func (a *DomainAlias) Query(ctx context.Context, domain string) (*Alias, error) {
	get, err := a.config.Get(ctx, "domain/alias/"+domain)
	if err != nil {
		return nil, err
	}
	rel := &Alias{}
	if err = json.Unmarshal([]byte(get), rel); err != nil {
		return nil, err
	}
	return rel, nil
}

func (a *DomainAlias) Bind(ctx context.Context, domains []string, owner, repo, branch string) error {
	oldDomains := make([]string, 0)
	rKey := fmt.Sprintf("domain/r-alias/%s/%s/%s", owner, repo, branch)
	if oldStr, err := a.config.Get(ctx, rKey); err == nil {
		_ = json.Unmarshal([]byte(oldStr), &oldDomains)
	}
	for _, oldDomain := range oldDomains {
		if err := a.Unbind(ctx, oldDomain); err != nil {
			return err
		}
	}
	if domains == nil || len(domains) == 0 {
		return nil
	}
	aliasMeta := &Alias{
		Owner:  owner,
		Repo:   repo,
		Branch: branch,
	}
	aliasMetaRaw, _ := json.Marshal(aliasMeta)
	domainsRaw, _ := json.Marshal(domains)
	_ = a.config.Put(ctx, rKey, string(domainsRaw), config.TtlKeep)
	for _, domain := range domains {
		if err := a.config.Put(ctx, "domain/alias/"+domain, string(aliasMetaRaw), config.TtlKeep); err != nil {
			return err
		}
	}
	return nil
}

func (a *DomainAlias) Unbind(ctx context.Context, domain string) error {
	return a.config.Delete(ctx, "domain/alias/"+domain)
}
