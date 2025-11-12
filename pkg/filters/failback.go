package filters

import (
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

var FilterInstFailback core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
	var param struct {
		Path string `json:"path"`
	}
	if err := config.Unmarshal(&param); err != nil {
		return nil, err
	}
	if param.Path == "" {
		return nil, errors.Errorf("filter failback: path is empty")
	}
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, metadata *core.PageDomainContent, next core.NextCall) error {
		err := next(ctx, writer, request, metadata)
		if (err != nil && !errors.Is(err, os.ErrNotExist)) || err == nil {
			return err
		}
		resp, err := metadata.NativeOpen(ctx, param.Path, nil)
		if resp != nil {
			defer resp.Body.Close()
		}
		if err != nil {
			return err
		}
		writer.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(param.Path)))
		lastMod, err := time.Parse(http.TimeFormat, resp.Header.Get("Last-Modified"))
		if err == nil {
			if seeker, ok := resp.Body.(io.ReadSeeker); ok {
				http.ServeContent(writer, request, filepath.Base(param.Path), lastMod, seeker)
				return nil
			}
		}
		_, err = io.Copy(writer, resp.Body)
		return err
	}, nil
}
