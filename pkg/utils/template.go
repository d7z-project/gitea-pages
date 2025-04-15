package utils

import (
	"net/http"
	"strings"
)

func NewTemplateInject(r *http.Request, def map[string]any) map[string]any {
	if def == nil {
		def = make(map[string]any)
	}
	headers := make(map[string]string)
	for k, vs := range r.Header {
		headers[k] = strings.Join(vs, ",")
	}
	def["Request"] = map[string]any{
		"Headers":    headers,
		"Path":       r.URL.Path,
		"Method":     r.Method,
		"RequestURI": r.RequestURI,
		"RemoteAddr": r.RemoteAddr,
		"RemoteIP":   GetRemoteIP(r),
	}
	return def
}
