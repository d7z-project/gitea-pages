package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"gopkg.d7z.net/middleware/kv"
)

type Alias struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

type DomainAlias struct {
	config kv.KV
}

func NewDomainAlias(config kv.KV) *DomainAlias {
	return &DomainAlias{config: config}
}

func (a *DomainAlias) Query(ctx context.Context, domain string) (*Alias, error) {
	get, err := a.config.Get(ctx, domain)
	if err != nil {
		return nil, err
	}
	rel := &Alias{}
	if err = json.Unmarshal([]byte(get), rel); err != nil {
		return nil, err
	}
	return rel, nil
}

func (a *DomainAlias) Bind(ctx context.Context, domains []string, owner, repo string) error {
	rKey := base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%s/%s", owner, repo)))

	var oldDomains []string
	domainsRaw, _ := json.Marshal(domains)

	for {
		success, err := a.config.PutIfNotExists(ctx, rKey, string(domainsRaw), kv.TTLKeep)
		if err != nil {
			return err
		}
		if success {
			oldDomains = []string{}
			break
		}

		oldStr, err := a.config.Get(ctx, rKey)
		if err != nil {
			continue
		}

		if err = json.Unmarshal([]byte(oldStr), &oldDomains); err != nil {
			oldDomains = []string{}
		}

		success, err = a.config.CompareAndSwap(ctx, rKey, oldStr, string(domainsRaw))
		if err != nil {
			return err
		}
		if success {
			break
		}
	}

	newDomainsMap := make(map[string]bool)
	for _, d := range domains {
		newDomainsMap[d] = true
	}

	for _, oldDomain := range oldDomains {
		if !newDomainsMap[oldDomain] {
			_ = a.Unbind(ctx, oldDomain)
		}
	}

	if len(domains) == 0 {
		return nil
	}
	aliasMeta := &Alias{
		Owner: owner,
		Repo:  repo,
	}
	aliasMetaRaw, _ := json.Marshal(aliasMeta)
	for _, domain := range domains {
		_ = a.config.Put(ctx, domain, string(aliasMetaRaw), kv.TTLKeep)
	}
	return nil
}

func (a *DomainAlias) Unbind(ctx context.Context, domain string) error {
	_, err := a.config.Delete(ctx, domain)
	return err
}
