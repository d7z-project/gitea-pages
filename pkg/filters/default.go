package filters

import (
	"io"
	"net/http"
	"os"

	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var FilterInstDefaultNotFound core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
	return func(ctx core.FilterContext, writer http.ResponseWriter, request *http.Request, next core.NextCall) error {
		err := next(ctx, writer, request)
		if err != nil && errors.Is(err, os.ErrNotExist) {
			open, err := ctx.NativeOpen(ctx, "/404.html", nil)
			if open != nil {
				defer open.Body.Close()
			}
			if err != nil {
				return err
			}
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			if l := open.Header.Get("Content-Length"); l != "" {
				writer.Header().Set("Content-Length", l)
			}
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.Copy(writer, open.Body)
			return nil
		}
		return err
	}, nil
}
