package core

import (
	"encoding/json"

	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

type Alias struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

type DomainAlias struct {
	config utils.Config
}

func NewDomainAlias(config utils.Config) *DomainAlias {
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

func (a *DomainAlias) Bind(domain, owner, repo, branch string) error {
	save := &Alias{
		Owner:  owner,
		Repo:   repo,
		Branch: branch,
	}
	saveB, err := json.Marshal(save)
	if err != nil {
		return err
	}
	return a.config.Put("domain/alias/"+domain, string(saveB), utils.TtlKeep)
}

func (a *DomainAlias) Unbind(domain string) error {
	return a.config.Delete("domain/alias/" + domain)
}
