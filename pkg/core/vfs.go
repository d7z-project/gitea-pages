package core

import (
	"context"
	"io"
	"net/http"
	"os"
)

type PageVFS struct {
	backend Backend

	org      string
	repo     string
	commitID string
}

// MaxFileLoadSize limits the maximum size of file loaded into memory (10MB)
const MaxFileLoadSize = 10 * 1024 * 1024

func NewPageVFS(
	backend Backend,
	org string,
	repo string,
	commitID string,
) *PageVFS {
	return &PageVFS{
		backend:  backend,
		org:      org,
		repo:     repo,
		commitID: commitID,
	}
}

func (p *PageVFS) NativeOpen(ctx context.Context, path string, headers http.Header) (*http.Response, error) {
	return p.backend.Open(ctx, p.org, p.repo, p.commitID, path, headers)
}

func (p *PageVFS) Exists(ctx context.Context, path string) (bool, error) {
	open, err := p.NativeOpen(ctx, path, nil)
	if open != nil {
		defer open.Body.Close()
	}
	if err != nil || open == nil {
		return false, err
	}
	if open.StatusCode != http.StatusOK {
		return false, nil
	}
	return true, nil
}

func (p *PageVFS) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	resp, err := p.NativeOpen(ctx, path, nil)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, os.ErrNotExist
	}
	return resp.Body, nil
}

func (p *PageVFS) Read(ctx context.Context, path string) ([]byte, error) {
	open, err := p.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer open.Close()

	// Use LimitReader to prevent reading too much data
	limitReader := io.LimitReader(open, MaxFileLoadSize+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, err
	}

	if len(data) > MaxFileLoadSize {
		return nil, &os.PathError{Op: "read", Path: path, Err: os.ErrInvalid} // Or a specific "file too large" error
	}

	return data, nil
}

func (p *PageVFS) ReadString(ctx context.Context, path string) (string, error) {
	read, err := p.Read(ctx, path)
	if err != nil {
		return "", err
	}
	return string(read), nil
}
