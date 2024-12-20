package providers

import "io"

type Blob struct {
	MediaType string
	io.ReadCloser
}

type Backend interface {
	Owners() ([]string, error)
	Repos(owner string) ([]string, error)
	Branches(owner, repo string) ([]string, error)
	Open(owner, repo, branch, path string) (Blob, error)
}
