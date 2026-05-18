package filters

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func applyCacheControl(header http.Header, ctx core.FilterContext, cacheControl string) {
	if cacheControl == "" || header.Get("Cache-Control") != "" {
		return
	}
	if (ctx.Private || ctx.Auth.Authenticated) && strings.HasPrefix(strings.ToLower(cacheControl), "public") {
		cacheControl = "private" + cacheControl[len("public"):]
	}
	header.Set("Cache-Control", cacheControl)
}

func writeStaticFileResponse(
	ctx core.FilterContext,
	writer http.ResponseWriter,
	request *http.Request,
	path string,
	resp *http.Response,
	cacheControl string,
) error {
	writer.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
	applyCacheControl(writer.Header(), ctx, cacheControl)
	lastMod, err := time.Parse(http.TimeFormat, resp.Header.Get("Last-Modified"))
	if err == nil {
		if seeker, ok := resp.Body.(io.ReadSeeker); ok {
			http.ServeContent(writer, request, filepath.Base(path), lastMod, seeker)
			return nil
		}
	}
	_, err = io.Copy(writer, resp.Body)
	return err
}
