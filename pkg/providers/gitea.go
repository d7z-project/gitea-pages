package providers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"

	"code.gitea.io/sdk/gitea"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

type ProviderGitea struct {
	BaseURL string
	Token   string

	gitea         *gitea.Client
	client        *http.Client
	defaultBranch string
}

func NewGitea(httpClient *http.Client, url, token, defaultBranch string) (*ProviderGitea, error) {
	client, err := gitea.NewClient(url, gitea.SetGiteaVersion(""), gitea.SetToken(token))
	if err != nil {
		return nil, err
	}
	return &ProviderGitea{
		BaseURL:       url,
		Token:         token,
		gitea:         client,
		client:        httpClient,
		defaultBranch: defaultBranch,
	}, nil
}

func (g *ProviderGitea) Meta(_ context.Context, owner, repo string) (*core.Metadata, error) {
	branch, resp, err := g.gitea.GetRepoBranch(owner, repo, g.defaultBranch)
	if err != nil {
		if resp != nil && resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return nil, errors.Join(err, os.ErrNotExist)
		}
		return nil, err
	}
	return &core.Metadata{
		ID:           branch.Commit.ID,
		LastModified: branch.Commit.Timestamp,
	}, nil
}

func (g *ProviderGitea) Open(ctx context.Context, owner, repo, commit, path string, headers http.Header) (*http.Response, error) {
	if headers == nil {
		headers = make(http.Header)
	}
	giteaURL, err := url.JoinPath(g.BaseURL, "api/v1/repos", owner, repo, "media", path)
	if err != nil {
		return nil, err
	}
	giteaURL += "?ref=" + url.QueryEscape(commit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, giteaURL, nil)
	if err != nil {
		return nil, err
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Add("Authorization", "token "+g.Token)
	return g.client.Do(req)
}

func (g *ProviderGitea) Close() error {
	return nil
}
