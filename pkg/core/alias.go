package core

import (
	"encoding/json"
	"fmt"

	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

type Alias struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

type DomainAlias struct {
	config utils.KVConfig
}

func NewDomainAlias(config utils.KVConfig) *DomainAlias {
	return &DomainAlias{config: config}
}

func (a *DomainAlias) Query(domain string) (*Alias, error) {
	get, err := a.config.Get("domain/alias/" + domain)
	if err != nil {
		return nil, err
	}
	rel := &Alias{}
	if err = json.Unmarshal([]byte(get), rel); err != nil {
		return nil, err
	}
	return rel, nil
}

func (a *DomainAlias) Bind(domains []string, owner, repo, branch string) error {
	oldDomains := make([]string, 0)
	rKey := fmt.Sprintf("domain/r-alias/%s/%s/%s", owner, repo, branch)
	if oldStr, err := a.config.Get(rKey); err == nil {
		_ = json.Unmarshal([]byte(oldStr), &oldDomains)
	}
	for _, oldDomain := range oldDomains {
		if err := a.Unbind(oldDomain); err != nil {
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
	_ = a.config.Put(rKey, string(domainsRaw), utils.TtlKeep)
	for _, domain := range domains {
		if err := a.config.Put("domain/alias/"+domain, string(aliasMetaRaw), utils.TtlKeep); err != nil {
			return err
		}
	}
	return nil
}

func (a *DomainAlias) Unbind(domain string) error {
	return a.config.Delete("domain/alias/" + domain)
}
