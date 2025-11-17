package goja

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"
	"github.com/dop251/goja_nodejs/url"
	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var FilterInstGoJa core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
	var param struct {
		Exec  string `json:"exec"`
		Debug bool   `json:"debug"`
	}
	if err := config.Unmarshal(&param); err != nil {
		return nil, err
	}
	if param.Exec == "" {
		return nil, errors.Errorf("no exec specified")
	}
	return func(ctx core.FilterContext, w http.ResponseWriter, request *http.Request, next core.NextCall) error {
		js, err := ctx.ReadString(ctx, param.Exec)
		if err != nil {
			return err
		}
		debug := NewDebug(param.Debug && request.URL.Query().Get("debug") == "true", request, w)
		registry := newRegistry(ctx)
		registry.RegisterNativeModule(console.ModuleName, console.RequireWithPrinter(debug))
		vm := goja.New()
		_ = registry.Enable(vm)
		console.Enable(vm)
		url.Enable(vm)
		if err = RequestInject(ctx, vm, request); err != nil {
			return err
		}
		if err = ResponseInject(vm, debug, request); err != nil {
			return err
		}
		if err = KVInject(ctx, vm); err != nil {
			return err
		}
		coreCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		go func() {
			select {
			case <-coreCtx.Done():
				vm.Interrupt(coreCtx.Err())
				return
			}
		}()
		_, err = vm.RunScript(param.Exec, js)
		cancel()
		return debug.Flush(err)
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
