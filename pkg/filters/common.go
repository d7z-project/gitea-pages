package filters

import (
	"errors"

	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/filters/goja"
)

func DefaultFilters(config map[string]map[string]any) (map[string]core.FilterInstance, error) {
	if config == nil {
		return nil, errors.New("config is nil")
	}
	result := make(map[string]core.FilterInstance)
	for key, instance := range map[string]core.GlobalFilter{
		"block":    FilterInstBlock,
		"redirect": FilterInstRedirect,
		"direct":   FilterInstDirect,
		//"reverse_proxy": FilterInstProxy,
		"404":      FilterInstDefaultNotFound,
		"failback": FilterInstFailback,
		"template": FilterInstTemplate,
		"js":       goja.FilterInstGoJa,
	} {
		item, ok := config[key]
		if !ok {
			item = make(map[string]any)
		}
		if it, ok := item["Enabled"]; ok && it == false {
			zap.L().Debug("skip filter", zap.String("key", key))
			continue
		}
		inst, err := instance(item)
		if err != nil {
			return nil, err
		}
		result[key] = inst
	}
	return result, nil
}
