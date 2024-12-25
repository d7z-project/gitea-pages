package services

import (
	"net/http"
)

type Backend interface {
	// Repos return repo name + default branch
	Repos(owner string) (map[string]string, error)
	// Branches return branch + commit id
	Branches(owner, repo string) (map[string]string, error)
	// Open return file or error
	Open(client http.Client, owner, repo, commit, path string, headers map[string]string) (*http.Response, error)
}
