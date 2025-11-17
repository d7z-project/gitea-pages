package goja

import (
	"os"
	"strings"

	"github.com/dop251/goja"
	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/middleware/kv"
)

func KVInject(ctx core.FilterContext, jsCtx *goja.Runtime) error {
	return jsCtx.GlobalObject().Set("kv", map[string]interface{}{
		"repo": func(group string) goja.Value {
			return kvResult(ctx.RepoDB)(ctx, jsCtx, group)
		},
		"org": func(group string) goja.Value {
			return kvResult(ctx.OrgDB)(ctx, jsCtx, group)
		},
	})
}

func kvResult(db kv.CursorPagedKV) func(ctx core.FilterContext, jsCtx *goja.Runtime, group string) goja.Value {
	return func(ctx core.FilterContext, jsCtx *goja.Runtime, group string) goja.Value {
		group = strings.TrimSpace(group)
		if group == "" {
			panic("kv: invalid group name")
		}
		db := db.Child(group)
		return jsCtx.ToValue(map[string]interface{}{
			"get": func(key string) goja.Value {
				get, err := db.Get(ctx, key)
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						panic(err)
					}
					return goja.Null()
				}
				return jsCtx.ToValue(get)
			},
			"set": func(key, value string) {
				err := db.Put(ctx, key, value, kv.TTLKeep)
				if err != nil {
					panic(err)
				}
			},
			"delete": func(key string) bool {
				b, err := db.Delete(ctx, key)
				if err != nil {
					panic(err)
				}
				return b
			},
			"putIfNotExists": func(key, value string) bool {
				exists, err := db.PutIfNotExists(ctx, key, value, kv.TTLKeep)
				if err != nil {
					panic(err)
				}
				return exists
			},
			"compareAndSwap": func(key, oldValue, newValue string) bool {
				swap, err := db.CompareAndSwap(ctx, key, oldValue, newValue)
				if err != nil {
					panic(err)
				}
				return swap
			},
		})
	}
}
