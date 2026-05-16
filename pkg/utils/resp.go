package utils

import (
	"bufio"
	"net"
	"net/http"
	"sync/atomic"

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

func IsWrittenResponseWriter(writer http.ResponseWriter) bool {
	if writer == nil {
		return false
	}
	if provider, ok := writer.(interface{ IsWritten() bool }); ok {
		return provider.IsWritten()
	}
	if provider, ok := writer.(BaseWriterProvider); ok {
		base := provider.BaseWriter()
		if base != nil && base != writer {
			return IsWrittenResponseWriter(base)
		}
	}
	return false
}

type WrittenResponseWriter struct {
	base    http.ResponseWriter
	root    http.ResponseWriter
	written atomic.Bool
}

func NewWrittenResponseWriter(base http.ResponseWriter) *WrittenResponseWriter {
	return &WrittenResponseWriter{
		base: base,
		root: BaseWriterOf(base),
	}
}

func (w *WrittenResponseWriter) Header() http.Header {
	return w.base.Header()
}

func (w *WrittenResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.written.Store(true)
	if hijacker, ok := w.base.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, errors.New("not hijackable")
}

func (w *WrittenResponseWriter) Write(b []byte) (int, error) {
	w.written.Store(true)
	return w.base.Write(b)
}

func (w *WrittenResponseWriter) WriteHeader(statusCode int) {
	w.written.Store(true)
	w.base.WriteHeader(statusCode)
}

func (w *WrittenResponseWriter) Flush() {
	w.written.Store(true)
	if flusher, ok := w.base.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *WrittenResponseWriter) IsWritten() bool {
	return w.written.Load()
}

func (w *WrittenResponseWriter) BaseWriter() http.ResponseWriter {
	return w.root
}

func (w *WrittenResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.base.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
