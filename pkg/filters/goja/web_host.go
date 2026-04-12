package goja

import (
	"os"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/middleware/kv"
)

func installHostGlobals(ctx core.FilterContext, vm *goja.Runtime, loop *eventloop.EventLoop, fsEnabled bool) (*goja.Object, error) {
	host := vm.NewObject()
	if err := host.Set("meta", map[string]any{
		"org":    ctx.Owner,
		"repo":   ctx.Repo,
		"commit": ctx.CommitID,
	}); err != nil {
		return nil, err
	}
	if err := host.Set("auth", map[string]any{
		"authenticated": ctx.Auth.Authenticated,
		"identity":      authIdentityMap(ctx.Auth.Identity),
	}); err != nil {
		return nil, err
	}
	if fsEnabled {
		if err := vm.Set("fs", map[string]any{
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
				return vm.ToValue(items), nil
			},
			"read": func(path string) *goja.Promise {
				promise, resolve, reject := vm.NewPromise()
				go func() {
					data, err := ctx.PageVFS.Read(ctx, path)
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						if err != nil {
							_ = reject(runtime.ToValue(err))
							return
						}
						_ = resolve(uint8ArrayValue(runtime, data))
					})
				}()
				return promise
			},
			"readText": func(path string) *goja.Promise {
				promise, resolve, reject := vm.NewPromise()
				go func() {
					data, err := ctx.PageVFS.ReadString(ctx, path)
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						if err != nil {
							_ = reject(runtime.ToValue(err))
							return
						}
						_ = resolve(runtime.ToValue(data))
					})
				}()
				return promise
			},
			"readSync": func(path string) (goja.Value, error) {
				data, err := ctx.PageVFS.Read(ctx, path)
				if err != nil {
					return nil, err
				}
				return uint8ArrayValue(vm, data), nil
			},
			"readTextSync": func(path string) (string, error) {
				return ctx.PageVFS.ReadString(ctx, path)
			},
		}); err != nil {
			return nil, err
		}
	}
	if err := vm.Set("kv", map[string]any{
		"repo": func(group ...string) (goja.Value, error) {
			return kvResult(ctx.RepoDB)(ctx, vm, group...)
		},
		"org": func(group ...string) (goja.Value, error) {
			return kvResult(ctx.OrgDB)(ctx, vm, group...)
		},
	}); err != nil {
		return nil, err
	}
	if err := vm.Set("event", map[string]any{
		"load": func(key string) *goja.Promise {
			promise, resolve, reject := vm.NewPromise()
			go func() {
				subscribe, err := ctx.Event.Subscribe(ctx, key)
				if err != nil {
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						_ = reject(runtime.ToValue(err))
					})
					return
				}
				select {
				case data := <-subscribe:
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						_ = resolve(runtime.ToValue(data))
					})
				case <-ctx.Done():
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						_ = reject(runtime.ToValue(ctx.Err()))
					})
				}
			}()
			return promise
		},
		"put": func(key, value string) *goja.Promise {
			promise, resolve, reject := vm.NewPromise()
			go func() {
				err := ctx.Event.Publish(ctx, key, value)
				loop.RunOnLoop(func(runtime *goja.Runtime) {
					if err != nil {
						_ = reject(runtime.ToValue(err))
					} else {
						_ = resolve(goja.Undefined())
					}
				})
			}()
			return promise
		},
	}); err != nil {
		return nil, err
	}
	if err := vm.Set("page", host); err != nil {
		return nil, err
	}
	return host, nil
}

func authIdentityMap(identity *core.AuthIdentity) any {
	if identity == nil {
		return nil
	}
	return map[string]any{
		"subject": identity.Subject,
		"name":    identity.Name,
	}
}

func kvResult(db kv.KV) func(ctx core.FilterContext, jsCtx *goja.Runtime, group ...string) (goja.Value, error) {
	return func(ctx core.FilterContext, jsCtx *goja.Runtime, group ...string) (goja.Value, error) {
		if len(group) == 0 {
			return goja.Undefined(), errors.New("invalid group")
		}
		db := db.Child(group...)
		return jsCtx.ToValue(map[string]any{
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
				t := time.Duration(kv.TTLKeep)
				if len(ttl) > 0 && ttl[0] > 0 {
					t = time.Duration(ttl[0]) * time.Millisecond
				}
				return db.Put(ctx, key, value, t)
			},
			"delete": func(key string) (bool, error) {
				return db.Delete(ctx, key)
			},
			"putIfNotExists": func(key, value string, ttl ...int) (bool, error) {
				t := time.Duration(kv.TTLKeep)
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
				list, err := db.ListCurrentCursor(ctx, &kv.ListOptions{Limit: limit, Cursor: cursor})
				if err != nil {
					return nil, err
				}
				keys := make([]string, len(list.Pairs))
				items := make([]map[string]string, len(list.Pairs))
				for i, p := range list.Pairs {
					keys[i] = p.Key
					items[i] = map[string]string{"key": p.Key, "value": p.Value}
				}
				return map[string]any{
					"keys":    keys,
					"items":   items,
					"cursor":  list.Cursor,
					"hasNext": list.HasMore,
				}, nil
			},
		}), nil
	}
}
