package goja

import (
	"io"
	"net/http"
	"strings"

	"github.com/dop251/goja"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func RequestInject(ctx core.FilterContext, jsCtx *goja.Runtime, req *http.Request) error {
	url := *req.URL
	url.Path = ctx.Path

	// 预计算头信息以提高性能
	headers := make(map[string]string)
	headerNames := make([]string, 0, len(req.Header))
	rawHeaderNames := make([]string, 0, len(req.Header))

	for key, values := range req.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
		headerNames = append(headerNames, strings.ToLower(key))
		rawHeaderNames = append(rawHeaderNames, key)
	}

	queryParams := make(map[string]string)
	for key, values := range url.Query() {
		if len(values) > 0 {
			queryParams[key] = values[0]
		}
	}
	return jsCtx.GlobalObject().Set("request", map[string]any{
		"method":      req.Method,
		"url":         url,
		"rawPath":     req.URL.Path,
		"host":        req.Host,
		"remoteAddr":  req.RemoteAddr,
		"proto":       req.Proto,
		"httpVersion": req.Proto,
		"path":        url.Path,
		"query":       queryParams,
		"headers":     headers,
		"get": func(key string) any {
			get := req.Header.Get(key)
			if get != "" {
				return get
			}
			return nil
		},
		"getHeader": func(name string) string {
			return req.Header.Get(name)
		},
		"getHeaderNames": func() []string {
			return headerNames
		},
		"getHeaders": func() map[string]string {
			return headers
		},
		"getRawHeaderNames": func() []string {
			return rawHeaderNames
		},
		"hasHeader": func(name string) bool {
			return req.Header.Get(name) != ""
		},
		"readBody": func() []byte {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				panic(err)
			}
			return body
		},
		"protocol": req.Proto,
	})
}
