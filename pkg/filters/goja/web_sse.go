package goja

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

type sseState struct {
	ctx     core.FilterContext
	writer  http.ResponseWriter
	flusher http.Flusher
	ready   chan struct{}
	done    chan struct{}
	once    sync.Once
	writeMu sync.Mutex
}

func installSSE(ctx core.FilterContext, vm *goja.Runtime, writer http.ResponseWriter) (io.Closer, error) {
	closers := NewClosers()
	if err := vm.Set("createEventStream", func(_ ...goja.Value) *goja.Object {
		state := &sseState{
			ctx:   ctx,
			ready: make(chan struct{}),
			done:  make(chan struct{}),
		}
		responseObj := newResponseObject(vm, &webResponseState{
			status: http.StatusOK,
			headers: http.Header{
				"Content-Type":      []string{"text/event-stream; charset=utf-8"},
				"Cache-Control":     []string{"no-cache"},
				"Connection":        []string{"keep-alive"},
				"X-Accel-Buffering": []string{"no"},
			},
			upgrade: func() error {
				base := unwrapResponseWriter(writer)
				flusher, ok := base.(http.Flusher)
				if !ok {
					return errors.New("response writer does not support streaming")
				}
				writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				writer.Header().Set("Cache-Control", "no-cache")
				writer.Header().Set("Connection", "keep-alive")
				writer.Header().Set("X-Accel-Buffering", "no")
				writer.WriteHeader(http.StatusOK)
				state.writer = writer
				state.flusher = flusher
				close(state.ready)
				flusher.Flush()
				select {
				case <-ctx.Done():
					state.finish()
					return ctx.Err()
				case <-state.done:
					return nil
				}
			},
		})
		streamObj := newSSEStreamObject(vm, state)
		result := vm.NewObject()
		_ = result.Set("stream", streamObj)
		_ = result.Set("response", responseObj)
		return result
	}); err != nil {
		return nil, err
	}
	return closers, nil
}

func newSSEStreamObject(vm *goja.Runtime, state *sseState) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("send", func(data string, options ...goja.Value) *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		go func() {
			select {
			case <-state.ready:
			case <-state.ctx.Done():
				resolveOrReject(vm, reject, resolve, state.ctx.Err())
				return
			case <-state.done:
				resolveOrReject(vm, reject, resolve, errors.New("event stream is closed"))
				return
			}
			payload := encodeSSEPayload(vm, data, options)
			state.writeMu.Lock()
			_, err := io.WriteString(state.writer, payload)
			if err == nil {
				state.flusher.Flush()
			}
			state.writeMu.Unlock()
			resolveOrReject(vm, reject, resolve, err)
		}()
		return promise
	})
	_ = obj.Set("close", func() {
		state.finish()
	})
	return obj
}

func encodeSSEPayload(vm *goja.Runtime, data string, options []goja.Value) string {
	eventName := ""
	eventID := ""
	retry := int64(0)
	if len(options) > 0 && options[0] != nil && !goja.IsUndefined(options[0]) && !goja.IsNull(options[0]) {
		obj := options[0].ToObject(vm)
		if obj != nil {
			if value := obj.Get("event"); value != nil && !goja.IsUndefined(value) && !goja.IsNull(value) {
				eventName = value.String()
			}
			if value := obj.Get("id"); value != nil && !goja.IsUndefined(value) && !goja.IsNull(value) {
				eventID = value.String()
			}
			if value := obj.Get("retry"); value != nil && !goja.IsUndefined(value) && !goja.IsNull(value) {
				retry = value.ToInteger()
			}
		}
	}
	var payload strings.Builder
	if eventName != "" {
		payload.WriteString("event: ")
		payload.WriteString(eventName)
		payload.WriteByte('\n')
	}
	if eventID != "" {
		payload.WriteString("id: ")
		payload.WriteString(eventID)
		payload.WriteByte('\n')
	}
	if retry > 0 {
		payload.WriteString(fmt.Sprintf("retry: %d\n", retry))
	}
	for _, line := range splitSSELines(data) {
		payload.WriteString("data: ")
		payload.WriteString(line)
		payload.WriteByte('\n')
	}
	payload.WriteByte('\n')
	return payload.String()
}

func splitSSELines(data string) []string {
	if data == "" {
		return []string{""}
	}
	lines := make([]string, 0)
	current := ""
	for _, r := range data {
		if r == '\n' {
			lines = append(lines, current)
			current = ""
			continue
		}
		if r != '\r' {
			current += string(r)
		}
	}
	lines = append(lines, current)
	return lines
}

func (s *sseState) finish() {
	s.once.Do(func() {
		close(s.done)
	})
}

func unwrapResponseWriter(writer http.ResponseWriter) http.ResponseWriter {
	type unwrapper interface {
		Unwrap() http.ResponseWriter
	}
	for {
		item, ok := writer.(unwrapper)
		if !ok {
			return writer
		}
		next := item.Unwrap()
		if next == nil || next == writer {
			return writer
		}
		writer = next
	}
}

func resolveOrReject(vm *goja.Runtime, reject, resolve func(any) error, err error) {
	if err != nil {
		_ = reject(vm.ToValue(err.Error()))
		return
	}
	_ = resolve(goja.Undefined())
}
