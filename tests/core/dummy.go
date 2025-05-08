package core

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.d7z.net/gitea-pages/pkg/core"
)

type ProviderDummy struct {
	BaseDir string `yaml:"workdir"`
}

func NewDummy() (*ProviderDummy, error) {
	temp, err := os.MkdirTemp("", "dummy")
	if err != nil {
		return nil, err
	}
	return &ProviderDummy{
		BaseDir: temp,
	}, nil
}

func (p *ProviderDummy) Repos(owner string) (map[string]string, error) {
	dir, err := os.ReadDir(filepath.Join(p.BaseDir, owner))
	if err != nil {
		return nil, err
	}
	repos := make(map[string]string)
	for _, d := range dir {
		if d.IsDir() {
			repos[d.Name()] = "main"
		}
	}
	return repos, nil
}

func (p *ProviderDummy) Branches(owner, repo string) (map[string]*core.BranchInfo, error) {
	dir, err := os.ReadDir(filepath.Join(p.BaseDir, owner, repo))
	if err != nil {
		return nil, err
	}
	branches := make(map[string]*core.BranchInfo)
	for _, d := range dir {
		if d.IsDir() {
			branches[d.Name()] = &core.BranchInfo{
				ID:           d.Name(),
				LastModified: time.Time{},
			}
		}
	}
	return branches, nil
}

func (p *ProviderDummy) Open(_ *http.Client, owner, repo, commit, path string, _ http.Header) (*http.Response, error) {
	open, err := os.Open(filepath.Join(p.BaseDir, owner, repo, commit, path))
	if err != nil {
		return nil, err
	}
	all, err := io.ReadAll(open)
	defer open.Close()
	recorder := httptest.NewRecorder()
	recorder.Body = bytes.NewBuffer(all)
	recorder.Header().Add("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
	stat, _ := open.Stat()
	recorder.Header().Add("Content-Length", strconv.FormatInt(stat.Size(), 10))
	recorder.Header().Add("Last-Modified", stat.ModTime().Format(http.TimeFormat))
	return recorder.Result(), nil
}

func (p *ProviderDummy) Close() error {
	return os.RemoveAll(p.BaseDir)
}
