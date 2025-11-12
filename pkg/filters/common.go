package filters

import "gopkg.d7z.net/gitea-pages/pkg/core"

func DefaultFilters() map[string]core.FilterInstance {
	return map[string]core.FilterInstance{
		"redirect":          FilterInstRedirect,
		"direct":            FilterInstDirect,
		"reverse_proxy":     FilterInstProxy,
		"default_not_found": FilterInstDefaultNotFound,
	}
}
