package goja

import (
	"github.com/dop251/goja"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func EventInject(ctx core.FilterContext, jsCtx *goja.Runtime) error {
	return jsCtx.GlobalObject().Set("event", map[string]interface{}{
		"subscribe": func(key string) (map[string]any, error) {
			subscribe, err := ctx.Event.Subscribe(ctx, key)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"on": func(f func(string)) {
					go func() {
					z:
						for {
							select {
							case <-ctx.Done():
								break z
							case data := <-subscribe:
								f(data)
							}
						}
					}()
				},
				"get": func() (string, error) {
					select {
					case <-ctx.Done():
						return "", ctx.Err()
					case data := <-subscribe:
						return data, nil
					}
				},
			}, nil
		},
		"put": func(key, value string) error {
			return ctx.Event.Publish(ctx, key, value)
		},
	})
}
