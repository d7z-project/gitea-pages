package goja

import (
	"net/http"
	"path/filepath"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
	"github.com/dop251/goja_nodejs/url"
	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var FilterInstGoja core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
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
	return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
		js, err := ctx.ReadString(ctx, param.Exec)
		if err != nil {
			return err
		}
		newRegistry(ctx)
		registry := newRegistry(ctx)
		vm := goja.New()
		_ = registry.Enable(vm)
		url.Enable(vm)
		vm.GlobalObject().Set("")
		return nil
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
