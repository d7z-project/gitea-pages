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

type Backend interface {
	io.Closer
	Meta(ctx context.Context, owner, repo string) (*Metadata, error)
	// Open return file or error (error)
	Open(ctx context.Context, owner, repo, id, path string, headers http.Header) (*http.Response, error)
}
