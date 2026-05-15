package pkg

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func applyRequestSecurity(req *http.Request, security core.SecurityResult) {
	if req == nil || security.AllowRequestCookies {
		return
	}
	req.Header.Del("Cookie")
}

func handleSecurityPreflight(w http.ResponseWriter, security core.SecurityResult) bool {
	if !security.IsPreflight {
		return false
	}
	if security.AllowedOrigin == "" {
		http.Error(w, "cors origin is not allowed", http.StatusForbidden)
		return true
	}
	writeSecurityHeaders(w.Header(), security)
	w.WriteHeader(http.StatusNoContent)
	return true
}

func enforceRequestSecurity(w http.ResponseWriter, req *http.Request, security core.SecurityResult) bool {
	if handleSecurityPreflight(w, security) {
		return true
	}
	if strings.TrimSpace(req.Header.Get("Origin")) != "" && security.AllowedOrigin == "" {
		http.Error(w, "cors origin is not allowed", http.StatusForbidden)
		return true
	}
	if isWebSocketUpgrade(req) && !security.AllowWebSocket {
		http.Error(w, "websocket origin is not allowed", http.StatusForbidden)
		return true
	}
	return false
}

func isWebSocketUpgrade(req *http.Request) bool {
	if !strings.EqualFold(strings.TrimSpace(req.Header.Get("Upgrade")), "websocket") {
		return false
	}
	for _, value := range req.Header.Values("Connection") {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "Upgrade") {
				return true
			}
		}
	}
	return false
}

type securityResponseWriter struct {
	http.ResponseWriter
	security core.SecurityResult
	wrote    bool
}

func (w *securityResponseWriter) BaseWriter() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *securityResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *securityResponseWriter) WriteHeader(statusCode int) {
	if !w.wrote {
		w.wrote = true
		headers := w.ResponseWriter.Header()
		writeSecurityHeaders(headers, w.security)
		if !w.security.AllowResponseCookies {
			headers.Del("Set-Cookie")
		}
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *securityResponseWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *securityResponseWriter) Flush() {
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	flusher.Flush()
}

func (w *securityResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *securityResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func writeSecurityHeaders(headers http.Header, security core.SecurityResult) {
	if headers == nil {
		return
	}
	if security.AllowedOrigin != "" && headers.Get("Access-Control-Allow-Origin") == "" {
		headers.Set("Access-Control-Allow-Origin", security.AllowedOrigin)
		addVaryHeader(headers, "Origin")
		if security.AllowMethods != "" {
			headers.Set("Access-Control-Allow-Methods", security.AllowMethods)
		}
		if security.AllowHeaders != "" {
			headers.Set("Access-Control-Allow-Headers", security.AllowHeaders)
		}
		if security.ExposeHeaders != "" {
			headers.Set("Access-Control-Expose-Headers", security.ExposeHeaders)
		}
		if security.MaxAge > 0 {
			headers.Set("Access-Control-Max-Age", strconv.Itoa(security.MaxAge))
		}
		if security.AllowCredentials {
			headers.Set("Access-Control-Allow-Credentials", "true")
		}
	}
	if security.ResourcePolicy != "" && headers.Get("Cross-Origin-Resource-Policy") == "" {
		headers.Set("Cross-Origin-Resource-Policy", security.ResourcePolicy)
	}
	if security.FrameOptions != "" && headers.Get("X-Frame-Options") == "" {
		headers.Set("X-Frame-Options", security.FrameOptions)
	}
}

func addVaryHeader(headers http.Header, value string) {
	for _, item := range headers.Values("Vary") {
		for _, part := range strings.Split(item, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	headers.Add("Vary", value)
}
