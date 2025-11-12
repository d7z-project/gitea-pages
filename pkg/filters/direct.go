package filters

import (
	"context"
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

var FilterInstDirect core.FilterInstance = func(config core.FilterParams) (core.FilterCall, error) {
	var param struct {
		Prefix string `json:"prefix"`
	}
	if err := config.Unmarshal(&param); err != nil {
		return nil, err
	}
	param.Prefix = strings.Trim(param.Prefix, "/") + "/"
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, metadata *core.PageDomainContent, next core.NextCall) error {
		err := next(ctx, writer, request, metadata)
		if (err != nil && !errors.Is(err, os.ErrNotExist)) || err == nil {
			return err
		}
		var resp *http.Response
		var path string
		defaultPath := param.Prefix + strings.TrimSuffix(metadata.Path, "/")
		for _, p := range []string{defaultPath, defaultPath + "/index.html"} {
			zap.L().Debug("direct fetch", zap.String("path", p))
			resp, err = metadata.NativeOpen(request.Context(), p, nil)
			if err != nil {
				if resp != nil {
					resp.Body.Close()
				}
				if !errors.Is(err, os.ErrNotExist) {
					zap.L().Debug("error", zap.Any("error", err))
					return err
				}
				continue
			}
			path = p
			break
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
}
