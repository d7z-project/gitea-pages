package core

import (
	"context"
	"io"
	"net/http"
	"time"
)

type Metadata struct {
	ID           string    `json:"id"`
	LastModified time.Time `json:"last_modified"`
}

type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
}

type Backend interface {
	io.Closer
	Meta(ctx context.Context, owner, repo string) (*Metadata, error)
	// Open return file or error (error)
	Open(ctx context.Context, owner, repo, id, path string, headers http.Header) (*http.Response, error)
	List(ctx context.Context, owner, repo, id, path string) ([]DirEntry, error)
}
