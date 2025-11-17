package filters

import (
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/filters/goja"
)

func DefaultFilters() map[string]core.FilterInstance {
	return map[string]core.FilterInstance{
		"block":    FilterInstBlock,
		"redirect": FilterInstRedirect,
		"direct":   FilterInstDirect,
		//"reverse_proxy": FilterInstProxy,
		"_404_":    FilterInstDefaultNotFound,
		"failback": FilterInstFailback,
		"template": FilterInstTemplate,
		"js":       goja.FilterInstGoJa,
	}
}
