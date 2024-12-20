package providers

type ProviderGitea struct {
}

func (g *ProviderGitea) Owners() ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (g *ProviderGitea) Repos(owner string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (g *ProviderGitea) Branches(owner, repo string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (g *ProviderGitea) Open(owner, repo, branch, path string) (Blob, error) {
	//TODO implement me
	panic("implement me")
}
