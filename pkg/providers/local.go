package providers

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
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.d7z.net/gitea-pages/pkg/core"
)

type LocalProvider struct {
	graph   map[string][]string
	path    string
	overlay map[string][]byte
}

func NewLocalProvider(
	graph map[string][]string,
	path string,
) *LocalProvider {
	return &LocalProvider{
		graph:   graph,
		path:    path,
		overlay: map[string][]byte{},
	}
}

func (l *LocalProvider) AddOverlay(path string, overlay []byte) {
	l.overlay[strings.Trim(path, "/")] = overlay
}

func (l *LocalProvider) Close() error {
	return nil
}

func (l *LocalProvider) Repos(_ context.Context, owner string) (map[string]string, error) {
	item, ok := l.graph[owner]
	if !ok {
		return nil, os.ErrNotExist
	}
	result := make(map[string]string)
	for _, s := range item {
		result[s] = "gh-pages"
	}
	return result, nil
}

func (l *LocalProvider) Branches(_ context.Context, owner, repo string) (map[string]*core.BranchInfo, error) {
	item, ok := l.graph[owner]
	if !ok {
		return nil, os.ErrNotExist
	}
	if !slices.Contains(item, repo) {
		return nil, os.ErrNotExist
	}
	return map[string]*core.BranchInfo{
		"gh-pages": {
			ID:           "adc83b19e793491b1c6ea0fd8b46cd9f32e592fc",
			LastModified: time.Now(),
		},
	}, nil
}

func (l *LocalProvider) Open(_ context.Context, _, _, _, path string, _ http.Header) (*http.Response, error) {
	var all []byte
	recorder := httptest.NewRecorder()
	if data, ok := l.overlay[strings.Trim(path, "/")]; ok {
		all = data
		recorder.Header().Add("Content-Length", strconv.FormatInt(int64(len(data)), 10))
	} else {
		open, err := os.Open(filepath.Join(l.path, path))
		if err != nil {
			return nil, errors.Join(err, os.ErrNotExist)
		}
		defer open.Close()
		all, err = io.ReadAll(open)
		if err != nil {
			return nil, errors.Join(err, os.ErrNotExist)
		}
		stat, _ := open.Stat()
		recorder.Header().Add("Content-Length", strconv.FormatInt(stat.Size(), 10))
		recorder.Header().Add("Last-Modified", stat.ModTime().Format(http.TimeFormat))
	}

	recorder.Body = bytes.NewBuffer(all)
	recorder.Header().Add("Content-Type", mime.TypeByExtension(filepath.Ext(path)))

	return recorder.Result(), nil
}
