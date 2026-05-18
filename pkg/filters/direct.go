package filters

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func FilterInstDirect(init core.GlobalFilterInit) (core.FilterInstance, error) {
	return func(config core.Params) (core.FilterCall, error) {
		var param struct {
			Prefix string `json:"prefix"`
		}
		if err := config.Unmarshal(&param); err != nil {
			return nil, err
		}
		param.Prefix = strings.Trim(param.Prefix, "/") + "/"
		return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
			err := next(ctx, writer, request)
			if (err != nil && !errors.Is(err, os.ErrNotExist)) || err == nil {
				return err
			}
			if request.Method != http.MethodHead && request.Method != http.MethodGet {
				http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)
				return nil
			}
			path := param.Prefix + strings.TrimSuffix(ctx.Path, "/")
			slog.Debug("direct fetch", "path", path)
			resp, err := ctx.NativeOpen(request.Context(), path, nil)
			if err != nil {
				if resp != nil {
					resp.Body.Close()
				}
				if !errors.Is(err, os.ErrNotExist) {
					slog.Debug("error", "error", err)
					return err
				}
				exists, e := ctx.Exists(ctx, path+"/index.html")
				if e != nil {
					return err
				}
				if exists {
					http.Redirect(writer, request, strings.TrimSuffix(request.URL.Path, "/")+"/", http.StatusFound)
					return nil
				}
				return err
			}
			if resp == nil {
				return os.ErrNotExist
			}
			defer resp.Body.Close()
			if err != nil {
				return err
			}
			return writeStaticFileResponse(writer, request, path, resp, init.Server.StaticCacheControl)
		}, nil
	}, nil
}
