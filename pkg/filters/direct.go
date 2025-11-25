package filters

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func FilterInstDirect(_ core.Params) (core.FilterInstance, error) {
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
			zap.L().Debug("direct fetch", zap.String("path", path))
			resp, err := ctx.NativeOpen(request.Context(), path, nil)
			if err != nil {
				if resp != nil {
					resp.Body.Close()
				}
				if !errors.Is(err, os.ErrNotExist) {
					zap.L().Debug("error", zap.Any("error", err))
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

			writer.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
			lastMod, err := time.Parse(http.TimeFormat, resp.Header.Get("Last-Modified"))
			if err == nil {
				if seeker, ok := resp.Body.(io.ReadSeeker); ok {
					http.ServeContent(writer, request, filepath.Base(path), lastMod, seeker)
					return nil
				}
			}
			_, err = io.Copy(writer, resp.Body)
			return err
		}, nil
	}, nil
}
