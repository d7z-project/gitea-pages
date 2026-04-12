package goja

import (
	"github.com/dop251/goja"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func FSInject(ctx core.FilterContext, jsCtx *goja.Runtime) error {
	return jsCtx.GlobalObject().Set("fs", map[string]interface{}{
		"list": func(path ...string) (goja.Value, error) {
			target := ""
			if len(path) > 0 {
				target = path[0]
			}
			list, err := ctx.PageVFS.List(ctx, target)
			if err != nil {
				return nil, err
			}
			items := make([]map[string]any, len(list))
			for i, item := range list {
				items[i] = map[string]any{
					"name": item.Name,
					"path": item.Path,
					"type": item.Type,
					"size": item.Size,
				}
			}
			return jsCtx.ToValue(items), nil
		},
	})
}
