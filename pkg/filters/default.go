package filters

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var FilterInstDefaultNotFound core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, metadata *core.PageContent, next core.NextCall) error {
		err := next(ctx, writer, request, metadata)
		if err != nil && errors.Is(err, os.ErrNotExist) {
			open, err := metadata.NativeOpen(ctx, "/404.html", nil)
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
		}
		return nil
	}, nil
}
