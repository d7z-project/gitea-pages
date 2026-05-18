package filters

import (
	"net/http"
	"os"

	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func FilterInstFailback(init core.GlobalFilterInit) (core.FilterInstance, error) {
	return func(config core.Params) (core.FilterCall, error) {
		var param struct {
			Path string `json:"path"`
		}
		if err := config.Unmarshal(&param); err != nil {
			return nil, err
		}
		if param.Path == "" {
			return nil, errors.Errorf("filter failback: path is empty")
		}
		return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
			err := next(ctx, writer, request)
			if (err != nil && !errors.Is(err, os.ErrNotExist)) || err == nil {
				return err
			}
			resp, err := ctx.NativeOpen(ctx, param.Path, nil)
			if resp != nil {
				defer resp.Body.Close()
			}
			if err != nil {
				return err
			}
			return writeStaticFileResponse(writer, request, param.Path, resp, init.Server.StaticCacheControl)
		}, nil
	}, nil
}
