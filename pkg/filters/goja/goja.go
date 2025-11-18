package goja

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/dop251/goja_nodejs/require"
	"github.com/dop251/goja_nodejs/url"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func FilterInstGoJa(_ core.Params) (core.FilterInstance, error) {
	return func(config core.Params) (core.FilterCall, error) {
		var param struct {
			Exec  string `json:"exec"`
			Debug bool   `json:"debug"`
		}
		if err := config.Unmarshal(&param); err != nil {
			return nil, err
		}
		if param.Exec == "" {
			return nil, errors.New("no exec specified")
		}
		return func(ctx core.FilterContext, w http.ResponseWriter, request *http.Request, next core.NextCall) error {
			js, err := ctx.ReadString(ctx, param.Exec)
			if err != nil {
				return err
			}
			prg, err := goja.Compile("main.js", js, false)
			if err != nil {
				return err
			}
			debug := NewDebug(param.Debug && request.URL.Query().Get("debug") == "true", request, w)
			registry := newRegistry(ctx)
			registry.RegisterNativeModule(console.ModuleName, console.RequireWithPrinter(debug))
			loop := eventloop.NewEventLoop(eventloop.WithRegistry(registry), eventloop.EnableConsole(true))
			stop := make(chan struct{}, 1)
			shutdown := make(chan struct{}, 1)
			timeout, cancelFunc := context.WithTimeout(ctx, 3*time.Second)
			defer cancelFunc()
			count := 0
			go func() {
				defer func() {
					shutdown <- struct{}{}
					close(shutdown)
				}()
				select {
				case <-timeout.Done():
				case <-stop:
				}
				count = loop.Stop()
			}()
			loop.Run(func(vm *goja.Runtime) {
				url.Enable(vm)
				if err = RequestInject(ctx, vm, request); err != nil {
					panic(err)
				}
				if err = ResponseInject(vm, debug, request); err != nil {
					panic(err)
				}
				if err = KVInject(ctx, vm); err != nil {
					panic(err)
				}
				_, err = vm.RunProgram(prg)
			})
			stop <- struct{}{}
			close(stop)
			<-shutdown
			if count != 0 {
				err = errors.Join(context.DeadlineExceeded, err)
			}
			return debug.Flush(err)
		}, nil
	}, nil
}

func newRegistry(ctx core.FilterContext) *require.Registry {
	registry := require.NewRegistry(
		require.WithLoader(func(path string) ([]byte, error) {
			return ctx.PageVFS.Read(ctx, path)
		}),
		require.WithPathResolver(func(base, path string) string {
			return filepath.Join(base, filepath.FromSlash(path))
		}))
	return registry
}
