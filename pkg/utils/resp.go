package utils

import (
	"bufio"
	"net"
	"net/http"

	"github.com/pkg/errors"
)

type BaseWriterProvider interface {
	BaseWriter() http.ResponseWriter
}

func BaseWriterOf(writer http.ResponseWriter) http.ResponseWriter {
	if provider, ok := writer.(BaseWriterProvider); ok {
		return provider.BaseWriter()
	}
	return writer
}

type WrittenResponseWriter struct {
	write bool
	base  http.ResponseWriter
	root  http.ResponseWriter
}

func NewWrittenResponseWriter(base http.ResponseWriter) *WrittenResponseWriter {
	return &WrittenResponseWriter{
		base:  base,
		root:  BaseWriterOf(base),
		write: false,
	}
}

func (w *WrittenResponseWriter) Header() http.Header {
	return w.base.Header()
}

func (w *WrittenResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.write = true
	if hijacker, ok := w.base.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, errors.New("not hijackable")
}

func (w *WrittenResponseWriter) Write(b []byte) (int, error) {
	w.write = true
	return w.base.Write(b)
}

func (w *WrittenResponseWriter) WriteHeader(statusCode int) {
	w.write = true
	w.base.WriteHeader(statusCode)
}

func (w *WrittenResponseWriter) Flush() {
	if flusher, ok := w.base.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *WrittenResponseWriter) IsWritten() bool {
	return w.write
}

func (w *WrittenResponseWriter) BaseWriter() http.ResponseWriter {
	return w.root
}
