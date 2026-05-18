package filters

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"time"
)

func applyCacheControl(header http.Header, cacheControl string) {
	if cacheControl == "" || header.Get("Cache-Control") != "" {
		return
	}
	header.Set("Cache-Control", cacheControl)
}

func writeStaticFileResponse(
	writer http.ResponseWriter,
	request *http.Request,
	path string,
	resp *http.Response,
	cacheControl string,
) error {
	writer.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
	applyCacheControl(writer.Header(), cacheControl)
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
