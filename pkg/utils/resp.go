package utils

import "net/http"

type WrittenResponseWriter struct {
	write bool
	base  http.ResponseWriter
}

func NewWrittenResponseWriter(base http.ResponseWriter) *WrittenResponseWriter {
	return &WrittenResponseWriter{
		base:  base,
		write: false,
	}
}

func (w *WrittenResponseWriter) Header() http.Header {
	return w.base.Header()
}

func (w *WrittenResponseWriter) Write(b []byte) (int, error) {
	w.write = true
	return w.base.Write(b)
}

func (w *WrittenResponseWriter) WriteHeader(statusCode int) {
	w.write = true
	w.base.WriteHeader(statusCode)
}

func (w *WrittenResponseWriter) IsWritten() bool {
	return w.write
}
