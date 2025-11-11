package core

import (
	"net/http"
	"regexp"
	"strings"
)

type Domain struct {
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"` // commit id or branch
	Path   string `json:"path"`
}

var portExp = regexp.MustCompile(`:\d+$`)

type DomainParser struct {
	baseDomain    string
	defaultBranch string
	alias         *DomainAlias
}

func (d *DomainParser) ParseDomains(request *http.Request) ([]Domain, error) {
	host := portExp.ReplaceAllString(strings.ToLower(request.Host), "")
	path := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	branch := request.URL.Query().Get("branch")
	if branch == "" {
		branch = d.defaultBranch
	}
	result := make([]Domain, 0)
	if strings.HasSuffix(host, d.baseDomain) {
		org := strings.TrimSuffix(host, d.baseDomain)
		if len(path) > 1 {
			// repo.base.com/path
			result = append(result, Domain{
				Org:    org,
				Repo:   path[0],
				Branch: branch,
				Path:   strings.Join(path[1:], "/"),
			})
		}
		// repo.base.com/
		result = append(result, Domain{
			Org:    org,
			Repo:   host,
			Branch: branch,
			Path:   strings.Join(path, "/"),
		})
	} else {
		if find, _ := d.alias.Query(request.Context(), host); find != nil {
			result = append(result, Domain{
				Org:    find.Owner,
				Repo:   find.Repo,
				Branch: find.Branch,
				Path:   request.URL.Path,
			})
		}
	}
	return result, nil
}
