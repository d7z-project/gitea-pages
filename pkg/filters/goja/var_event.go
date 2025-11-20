package goja

import (
	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func EventInject(ctx core.FilterContext, jsCtx *goja.Runtime, loop *eventloop.EventLoop) error {
	return jsCtx.GlobalObject().Set("event", map[string]interface{}{
		"load": func(key string) *goja.Promise {
			promise, resolve, reject := jsCtx.NewPromise()
			go func() {
				subscribe, err := ctx.Event.Subscribe(ctx, key)
				if err != nil {
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						_ = reject(runtime.ToValue(err))
					})
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
		"put": func(key, value string) error {
			return ctx.Event.Publish(ctx, key, value)
		},
	})
}
