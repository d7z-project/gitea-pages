package goja

import (
	"os"
	"time"

	"github.com/dop251/goja"
	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/middleware/kv"
)

func KVInject(ctx core.FilterContext, jsCtx *goja.Runtime) error {
	return jsCtx.GlobalObject().Set("kv", map[string]interface{}{
		"repo": func(group ...string) (goja.Value, error) {
			return kvResult(ctx.RepoDB)(ctx, jsCtx, group...)
		},
		"org": func(group ...string) (goja.Value, error) {
			return kvResult(ctx.OrgDB)(ctx, jsCtx, group...)
		},
	})
}

func kvResult(db kv.KV) func(ctx core.FilterContext, jsCtx *goja.Runtime, group ...string) (goja.Value, error) {
	return func(ctx core.FilterContext, jsCtx *goja.Runtime, group ...string) (goja.Value, error) {
		if len(group) == 0 {
			return goja.Undefined(), errors.New("invalid group")
		}
		db := db.Child(group...)
		return jsCtx.ToValue(map[string]interface{}{
			"get": func(key string) (goja.Value, error) {
				get, err := db.Get(ctx, key)
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						return nil, err
					}
					return goja.Null(), nil
				}
				return jsCtx.ToValue(get), nil
			},
			"set": func(key, value string, ttl ...int) error {
				var t time.Duration
				t = kv.TTLKeep
				if len(ttl) > 0 && ttl[0] > 0 {
					t = time.Duration(ttl[0]) * time.Millisecond
				}
				return db.Put(ctx, key, value, t)
			},
			"delete": func(key string) (bool, error) {
				return db.Delete(ctx, key)
			},
			"putIfNotExists": func(key, value string, ttl ...int) (bool, error) {
				var t time.Duration
				t = kv.TTLKeep
				if len(ttl) > 0 && ttl[0] > 0 {
					t = time.Duration(ttl[0]) * time.Millisecond
				}
				return db.PutIfNotExists(ctx, key, value, t)
			},
			"compareAndSwap": func(key, oldValue, newValue string) (bool, error) {
				return db.CompareAndSwap(ctx, key, oldValue, newValue)
			},
			"list": func(limit int64, cursor string) (map[string]any, error) {
				if limit <= 0 {
					limit = 100
				}
				list, err := db.CursorList(ctx, &kv.ListOptions{
					Limit:  limit,
					Cursor: cursor,
				})
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"keys":    list.Keys,
					"cursor":  list.Cursor,
					"hasNext": list.HasMore,
				}, nil
			},
		}), nil
	}
}
