package core

import (
	"context"
	"io"
	"net/http"
	"time"
)

type BranchInfo struct {
	ID           string    `json:"id"`
	LastModified time.Time `json:"last_modified"`
}

type Backend interface {
	io.Closer
	// Repos return repo name + default branch
	Repos(ctx context.Context, owner string) (map[string]string, error)
	// Branches return branch + commit id
	Branches(ctx context.Context, owner, repo string) (map[string]*BranchInfo, error)
	// Open return file or error (error)
	Open(ctx context.Context, client *http.Client, owner, repo, commit, path string, headers http.Header) (*http.Response, error)
}
