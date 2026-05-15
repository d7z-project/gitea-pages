package providers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.d7z.net/gitea-pages/pkg/core"
)

type LocalProvider struct {
	graph     map[string][]string
	path      string
	overlayMu sync.RWMutex
	overlay   map[string][]byte
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
	l.overlayMu.Lock()
	l.overlay[strings.Trim(path, "/")] = append([]byte(nil), overlay...)
	l.overlayMu.Unlock()
}

func (l *LocalProvider) Close() error {
	return nil
}

func (l *LocalProvider) Meta(_ context.Context, owner, repo string) (*core.Metadata, error) {
	if _, ok := l.graph[owner]; !ok {
		return nil, os.ErrNotExist
	}
	if !slices.Contains(l.graph[owner], repo) {
		return nil, os.ErrNotExist
	}

	return &core.Metadata{
		ID:           "localhost",
		LastModified: time.Now(),
	}, nil
}

func (l *LocalProvider) List(_ context.Context, _, _, _, path string) ([]core.DirEntry, error) {
	list, err := os.ReadDir(filepath.Join(l.path, path))
	if err != nil {
		return nil, errors.Join(err, os.ErrNotExist)
	}
	entries := make([]core.DirEntry, 0, len(list))
	for _, item := range list {
		entry := core.DirEntry{
			Name: item.Name(),
			Path: filepath.ToSlash(filepath.Join(path, item.Name())),
			Type: "file",
		}
		if item.IsDir() {
			entry.Type = "dir"
		} else if info, infoErr := item.Info(); infoErr == nil {
			entry.Size = info.Size()
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (l *LocalProvider) Open(_ context.Context, _, _, _, path string, _ http.Header) (*http.Response, error) {
	l.overlayMu.RLock()
	data, ok := l.overlay[strings.Trim(path, "/")]
	l.overlayMu.RUnlock()
	headers := make(http.Header)
	if ok {
		headers.Add("Content-Length", strconv.FormatInt(int64(len(data)), 10))
		headers.Add("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     headers,
			Body:       io.NopCloser(bytes.NewReader(append([]byte(nil), data...))),
		}, nil
	}

	open, err := os.Open(filepath.Join(l.path, path))
	if err != nil {
		return nil, errors.Join(err, os.ErrNotExist)
	}
	stat, err := open.Stat()
	if err != nil {
		_ = open.Close()
		return nil, errors.Join(err, os.ErrNotExist)
	}
	headers.Add("Content-Length", strconv.FormatInt(stat.Size(), 10))
	headers.Add("Last-Modified", stat.ModTime().Format(http.TimeFormat))
	headers.Add("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     headers,
		Body:       open,
	}, nil
}
