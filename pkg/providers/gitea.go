package providers

import (
	"net/http"
	"net/url"
	"os"

	"code.d7z.net/d7z-project/gitea-pages/pkg/core"

	"code.gitea.io/sdk/gitea"
)

const GiteaMaxCount = 9999

type ProviderGitea struct {
	BaseUrl string
	Token   string

	gitea *gitea.Client
}

func NewGitea(url string, token string) (*ProviderGitea, error) {
	client, err := gitea.NewClient(url, gitea.SetGiteaVersion(""), gitea.SetToken(token))
	if err != nil {
		return nil, err
	}
	return &ProviderGitea{
		BaseUrl: url,
		Token:   token,
		gitea:   client,
	}, nil
}

func (g *ProviderGitea) Repos(owner string) (map[string]string, error) {
	result := make(map[string]string)
	if repos, resp, err := g.gitea.ListOrgRepos(owner, gitea.ListOrgReposOptions{
		ListOptions: gitea.ListOptions{
			PageSize: GiteaMaxCount,
		},
	}); err != nil || (resp != nil && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound) {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	} else {
		if resp != nil {
			_ = resp.Body.Close()
		}
		for _, item := range repos {
			result[item.Name] = item.DefaultBranch
		}
	}
	if len(result) == 0 {
		if repos, resp, err := g.gitea.ListUserRepos(owner, gitea.ListReposOptions{
			ListOptions: gitea.ListOptions{
				PageSize: GiteaMaxCount,
			},
		}); err != nil || (resp != nil && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound) {
			if resp != nil {
				_ = resp.Body.Close()
			}
			return nil, err
		} else {
			if resp != nil {
				_ = resp.Body.Close()
			}
			for _, item := range repos {
				result[item.Name] = item.DefaultBranch
			}
		}
	}
	if len(result) == 0 {
		return nil, os.ErrNotExist
	}
	return result, nil
}

func (g *ProviderGitea) Branches(owner, repo string) (map[string]*core.BranchInfo, error) {
	result := make(map[string]*core.BranchInfo)
	if branches, resp, err := g.gitea.ListRepoBranches(owner, repo, gitea.ListRepoBranchesOptions{
		ListOptions: gitea.ListOptions{
			PageSize: GiteaMaxCount,
		},
	}); err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	} else {
		if resp != nil {
			_ = resp.Body.Close()
		}
		for _, branch := range branches {
			result[branch.Name] = &core.BranchInfo{
				ID:           branch.Commit.ID,
				LastModified: branch.Commit.Timestamp,
			}
		}
	}
	if len(result) == 0 {
		return nil, os.ErrNotExist
	}
	return result, nil
}

func (g *ProviderGitea) Open(client *http.Client, owner, repo, commit, path string, headers http.Header) (*http.Response, error) {
	giteaURL, err := url.JoinPath(g.BaseUrl, "api/v1/repos", owner, repo, "media", path)
	if err != nil {
		return nil, err
	}
	giteaURL += "?ref=" + url.QueryEscape(commit)
	req, err := http.NewRequest(http.MethodGet, giteaURL, nil)
	if err != nil {
		return nil, err
	}
	if headers != nil {
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}
	req.Header.Add("Authorization", "token "+g.Token)
	resp, err := client.Do(req)
	if err != nil && resp == nil {
		return nil, err
	}
	return resp, nil
}
