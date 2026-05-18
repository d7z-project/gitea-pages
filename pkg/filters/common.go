package filters

import (
	"errors"
	"log/slog"

	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/filters/goja"
)

var registeredFilters = map[string]core.GlobalFilter{
	"block":         FilterInstBlock,
	"redirect":      FilterInstRedirect,
	"direct":        FilterInstDirect,
	"reverse_proxy": FilterInstProxy,
	"404":           FilterInstDefaultNotFound,
	"failback":      FilterInstFailback,
	"template":      FilterInstTemplate,
	"js":            goja.FilterInstGoJa,
}

func DefaultFilters(config map[string]map[string]any, server core.FilterServerConfig) (map[string]core.FilterInstance, error) {
	if config == nil {
		return nil, errors.New("config is nil")
	}
	for key := range config {
		if _, ok := registeredFilters[key]; !ok {
			slog.Warn("unknown filter config is ignored", "filter", key)
		}
	}
	result := make(map[string]core.FilterInstance)
	for key, instance := range registeredFilters {
		item, ok := config[key]
		if !ok {
			item = make(map[string]any)
		}
		enabled, ok := item["enabled"].(bool)
		if !ok {
			enabled, ok = item["Enabled"].(bool)
		}
		if ok && !enabled {
			slog.Debug("skip filter", "key", key)
			continue
		}
		inst, err := instance(core.GlobalFilterInit{
			Config: item,
			Server: server,
		})
		if err != nil {
			return nil, err
		}
		result[key] = inst
	}
	return result, nil
}
