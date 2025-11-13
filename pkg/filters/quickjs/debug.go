package quickjs

import (
	"net/http"
	"strings"
	"time"
)

type DebugData struct {
	Status int            `json:"status"`
	Header http.Header    `json:"header"`
	Body   string         `json:"body"`
	Logs   []DebugDataLog `json:"logs"`
}

type DebugDataLog struct {
	Level   string    `json:"level"`
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

// debugResponseWriter 用于在 debug 模式下捕获响应输出
type debugResponseWriter struct {
	buffer *strings.Builder
	header http.Header
	status int
}

func (w *debugResponseWriter) Header() http.Header {
	return w.header
}

func (w *debugResponseWriter) Write(data []byte) (int, error) {
	return w.buffer.Write(data)
}

func (w *debugResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}
