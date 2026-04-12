package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"code.gitea.io/sdk/gitea"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

type GiteaConfig struct {
	Server            string   `json:"server"`
	Token             string   `json:"token"`
	DefaultBranch     string   `json:"-"`
	ClientID          string   `json:"client_id"`
	ClientSecret      string   `json:"client_secret"`
	RedirectURL       string   `json:"redirect_url"`
	Scopes            []string `json:"scopes"`
	AllowInsecureHTTP bool     `json:"allow_insecure_http"`
}

type ProviderGitea struct {
	BaseURL string
	Token   string

	gitea         *gitea.Client
	client        *http.Client
	defaultBranch string

	clientID          string
	clientSecret      string
	redirectURL       string
	scopes            []string
	allowInsecureHTTP bool
}

type giteaAuthSession struct {
	AccessToken string `json:"access_token"`
}

func init() {
	core.RegisterProvider("gitea", NewGiteaFromJSON)
}

func NewGiteaFromJSON(httpClient *http.Client, raw json.RawMessage, options core.ProviderOptions) (core.Provider, error) {
	var cfg GiteaConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, err
		}
	}
	cfg.DefaultBranch = options.DefaultBranch
	return NewGitea(httpClient, cfg)
}

func NewGitea(httpClient *http.Client, cfg GiteaConfig) (*ProviderGitea, error) {
	if cfg.Server == "" {
		return nil, errors.New("missing gitea server")
	}
	if cfg.ClientID != "" || cfg.ClientSecret != "" || cfg.RedirectURL != "" {
		if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
			return nil, errors.New("gitea auth requires client_id, client_secret and redirect_url together")
		}
		if err := validateAuthURL(cfg.Server, cfg.AllowInsecureHTTP); err != nil {
			return nil, fmt.Errorf("invalid auth server: %w", err)
		}
		if err := validateAuthURL(cfg.RedirectURL, cfg.AllowInsecureHTTP); err != nil {
			return nil, fmt.Errorf("invalid auth redirect_url: %w", err)
		}
	}
	client, err := gitea.NewClient(cfg.Server, gitea.SetGiteaVersion(""), gitea.SetToken(cfg.Token))
	if err != nil {
		return nil, err
	}
	return &ProviderGitea{
		BaseURL:           cfg.Server,
		Token:             cfg.Token,
		gitea:             client,
		client:            httpClient,
		defaultBranch:     cfg.DefaultBranch,
		clientID:          cfg.ClientID,
		clientSecret:      cfg.ClientSecret,
		redirectURL:       cfg.RedirectURL,
		scopes:            cfg.Scopes,
		allowInsecureHTTP: cfg.AllowInsecureHTTP,
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

func (g *ProviderGitea) List(_ context.Context, owner, repo, commit, path string) ([]core.DirEntry, error) {
	items, resp, err := g.gitea.ListContents(owner, repo, commit, path)
	if err != nil {
		if resp != nil && resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return nil, errors.Join(err, os.ErrNotExist)
		}
		return nil, err
	}
	entries := make([]core.DirEntry, len(items))
	for i, item := range items {
		entries[i] = core.DirEntry{
			Name: item.Name,
			Path: item.Path,
			Type: item.Type,
			Size: item.Size,
		}
	}
	return entries, nil
}

func (g *ProviderGitea) Close() error {
	return nil
}

func (g *ProviderGitea) AuthEnabled() bool {
	return g.clientID != "" && g.clientSecret != "" && g.redirectURL != ""
}

func (g *ProviderGitea) LoginURL(_ context.Context, state string) (string, error) {
	values := url.Values{}
	values.Set("client_id", g.clientID)
	values.Set("redirect_uri", g.redirectURL)
	values.Set("response_type", "code")
	values.Set("state", state)
	if len(g.scopes) > 0 {
		values.Set("scope", strings.Join(g.scopes, " "))
	}
	return strings.TrimRight(g.BaseURL, "/") + "/login/oauth/authorize?" + values.Encode(), nil
}

func (g *ProviderGitea) HandleCallback(ctx context.Context, req *http.Request) (*core.AuthSession, error) {
	if err := g.validateAuthConfig(); err != nil {
		return nil, err
	}
	code := req.URL.Query().Get("code")
	if code == "" {
		return nil, errors.New("missing code")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.clientSecret)
	form.Set("redirect_uri", g.redirectURL)

	tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.BaseURL, "/")+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenReq.Header.Set("Accept", "application/json")

	tokenResp, err := g.client.Do(tokenReq)
	if err != nil {
		return nil, err
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode < 200 || tokenResp.StatusCode >= 300 {
		return nil, errors.New("token exchange failed")
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err = json.NewDecoder(tokenResp.Body).Decode(&token); err != nil {
		return nil, err
	}
	if token.AccessToken == "" {
		return nil, errors.New("missing access token")
	}

	userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(g.BaseURL, "/")+"/api/v1/user", nil)
	if err != nil {
		return nil, err
	}
	userReq.Header.Set("Authorization", "token "+token.AccessToken)
	userResp, err := g.client.Do(userReq)
	if err != nil {
		return nil, err
	}
	defer userResp.Body.Close()
	if userResp.StatusCode < 200 || userResp.StatusCode >= 300 {
		return nil, errors.New("user lookup failed")
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err = json.NewDecoder(userResp.Body).Decode(&user); err != nil {
		return nil, err
	}
	privateData, err := json.Marshal(giteaAuthSession{AccessToken: token.AccessToken})
	if err != nil {
		return nil, err
	}
	return &core.AuthSession{
		Identity: core.AuthIdentity{
			Subject: strconv.FormatInt(user.ID, 10),
			Name:    user.Login,
		},
		Private: privateData,
	}, nil
}

func (g *ProviderGitea) AuthorizeRepo(ctx context.Context, sess *core.AuthSession, owner, repo string) (bool, error) {
	var private giteaAuthSession
	if err := json.Unmarshal(sess.Private, &private); err != nil {
		return false, err
	}
	repoURL := strings.TrimRight(g.BaseURL, "/") + "/api/v1/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	repoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, repoURL, nil)
	if err != nil {
		return false, err
	}
	repoReq.Header.Set("Authorization", "token "+private.AccessToken)
	repoResp, err := g.client.Do(repoReq)
	if err != nil {
		return false, err
	}
	defer repoResp.Body.Close()
	if repoResp.StatusCode == http.StatusOK {
		return true, nil
	}
	if repoResp.StatusCode == http.StatusForbidden || repoResp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, errors.New("repo authorization failed")
}

func (g *ProviderGitea) Logout(context.Context, *core.AuthSession) error {
	return nil
}

func (g *ProviderGitea) validateAuthConfig() error {
	if !g.AuthEnabled() {
		return errors.New("gitea auth is not fully configured")
	}
	if err := validateAuthURL(g.BaseURL, g.allowInsecureHTTP); err != nil {
		return err
	}
	return validateAuthURL(g.redirectURL, g.allowInsecureHTTP)
}

func validateAuthURL(raw string, allowInsecure bool) error {
	parse, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parse.Scheme == "" || parse.Host == "" {
		return errors.New("url must include scheme and host")
	}
	if parse.Scheme != "https" && !allowInsecure {
		return errors.New("non-https auth url is not allowed")
	}
	if parse.Scheme != "https" && parse.Scheme != "http" {
		return errors.New("unsupported scheme")
	}
	return nil
}
