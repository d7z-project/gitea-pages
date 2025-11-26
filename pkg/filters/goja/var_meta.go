package goja

import (
	"github.com/dop251/goja"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func MetaInject(ctx core.FilterContext, jsCtx *goja.Runtime) error {
	return jsCtx.GlobalObject().Set("meta", map[string]interface{}{
		"org":    ctx.Owner,
		"repo":   ctx.Repo,
		"commit": ctx.CommitID,
	})
}
