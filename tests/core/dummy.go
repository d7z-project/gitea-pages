package core

import (
	"bytes"
	"context"
	"errors"
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
	temp, err := os.MkdirTemp("", "dummy-*")
	if err != nil {
		return nil, err
	}
	return &ProviderDummy{
		BaseDir: temp,
	}, nil
}

func (p *ProviderDummy) Meta(_ context.Context, _, _ string) (*core.Metadata, error) {
	return &core.Metadata{
		ID:           "gh-pages",
		LastModified: time.Now(),
	}, nil
}

func (p *ProviderDummy) Open(_ context.Context, owner, repo, commit, path string, _ http.Header) (*http.Response, error) {
	open, err := os.Open(filepath.Join(p.BaseDir, owner, repo, commit, path))
	if err != nil {
		return nil, errors.Join(err, os.ErrNotExist)
	}
	defer open.Close()
	all, err := io.ReadAll(open)
	if err != nil {
		return nil, errors.Join(err, os.ErrNotExist)
	}
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
