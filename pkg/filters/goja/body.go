package goja

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	nurl "net/url"
	"strings"
	"sync/atomic"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

type bodySource interface {
	newStream() *readableStreamState
	clone() (bodySource, error)
	empty() bool
}

type bufferedBodySource struct {
	data []byte
}

type streamingBodySource struct {
	open func() (io.ReadCloser, error)
}

type webBodyState struct {
	contentType string
	used        *atomic.Bool
	stream      *readableStreamState
	loop        *eventloop.EventLoop
	runtime     *runtimeState
}

type webFormDataState struct {
	values map[string][]string
}

func newBufferedBodySource(data []byte) bodySource {
	if len(data) == 0 {
		return nil
	}
	return &bufferedBodySource{data: append([]byte(nil), data...)}
}

func newStreamingBodySource(open func() (io.ReadCloser, error)) bodySource {
	if open == nil {
		return nil
	}
	return &streamingBodySource{open: open}
}

func (s *bufferedBodySource) newStream() *readableStreamState {
	data := append([]byte(nil), s.data...)
	return &readableStreamState{
		open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		},
	}
}

func (s *bufferedBodySource) clone() (bodySource, error) {
	return &bufferedBodySource{data: append([]byte(nil), s.data...)}, nil
}

func (s *bufferedBodySource) empty() bool {
	return s == nil || len(s.data) == 0
}

func (s *streamingBodySource) newStream() *readableStreamState {
	return &readableStreamState{open: s.open}
}

func (s *streamingBodySource) clone() (bodySource, error) {
	return nil, errors.New("body stream cannot be cloned")
}

func (s *streamingBodySource) empty() bool {
	return s == nil || s.open == nil
}

func newBodyState(headers http.Header, used *atomic.Bool, source bodySource, loop *eventloop.EventLoop, runtime *runtimeState) *webBodyState {
	contentType := ""
	if headers != nil {
		contentType = headers.Get("Content-Type")
	}
	var stream *readableStreamState
	if source != nil && !source.empty() {
		stream = source.newStream()
	}
	return &webBodyState{
		contentType: contentType,
		used:        used,
		stream:      stream,
		loop:        loop,
		runtime:     runtime,
	}
}

func newBodyObject(vm *goja.Runtime, state *webBodyState) goja.Value {
	if state == nil || state.stream == nil {
		return goja.Null()
	}
	obj := vm.NewObject()
	_ = obj.Set("getReader", func() (*goja.Object, error) {
		if err := claimWebBody(state); err != nil {
			return nil, err
		}
		return newReadableStreamObject(vm, state.loop, state.runtime, state.stream), nil
	})
	return obj
}

func attachBodyMethods(vm *goja.Runtime, obj *goja.Object, state *webBodyState) {
	if obj == nil || state == nil {
		return
	}
	bodyPromise := func(transform func(*goja.Runtime, []byte) (goja.Value, error)) *goja.Promise {
		return asyncValuePromise(vm, state.loop, state.runtime, func() ([]byte, error) {
			return consumeWebBody(state)
		}, transform)
	}
	_ = obj.Set("body", newBodyObject(vm, state))
	_ = obj.DefineAccessorProperty("bodyUsed", vm.ToValue(func() bool {
		return state.used != nil && state.used.Load()
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.Set("text", func() *goja.Promise {
		return bodyPromise(func(vm *goja.Runtime, data []byte) (goja.Value, error) {
			return vm.ToValue(string(data)), nil
		})
	})
	_ = obj.Set("json", func() *goja.Promise {
		return bodyPromise(func(vm *goja.Runtime, data []byte) (goja.Value, error) {
			var decoded any
			if len(data) == 0 {
				return goja.Null(), nil
			}
			if err := json.Unmarshal(data, &decoded); err != nil {
				return nil, err
			}
			return vm.ToValue(decoded), nil
		})
	})
	_ = obj.Set("arrayBuffer", func() *goja.Promise {
		return bodyPromise(func(vm *goja.Runtime, data []byte) (goja.Value, error) {
			return vm.ToValue(vm.NewArrayBuffer(data)), nil
		})
	})
	_ = obj.Set("bytes", func() *goja.Promise {
		return bodyPromise(func(vm *goja.Runtime, data []byte) (goja.Value, error) {
			return uint8ArrayValue(vm, data), nil
		})
	})
	_ = obj.Set("blob", func() *goja.Promise {
		return bodyPromise(func(vm *goja.Runtime, data []byte) (goja.Value, error) {
			return vm.ToValue(newBlobObject(vm, data, state.contentType)), nil
		})
	})
	_ = obj.Set("formData", func() *goja.Promise {
		return bodyPromise(func(vm *goja.Runtime, data []byte) (goja.Value, error) {
			form, err := parseFormData(state.contentType, data)
			if err != nil {
				return nil, err
			}
			return vm.ToValue(newFormDataObject(vm, form)), nil
		})
	})
}

func claimWebBody(state *webBodyState) error {
	if state == nil {
		return nil
	}
	if state.used != nil && !state.used.CompareAndSwap(false, true) {
		return errors.New("body stream already read")
	}
	return nil
}

func consumeWebBody(state *webBodyState) ([]byte, error) {
	if state == nil || state.stream == nil {
		return nil, nil
	}
	if err := claimWebBody(state); err != nil {
		return nil, err
	}
	var data []byte
	for {
		chunk, done, err := state.stream.read(defaultStreamChunkSize)
		if err != nil {
			return nil, err
		}
		if done {
			return data, nil
		}
		data = append(data, chunk...)
	}
}

func newBlobObject(vm *goja.Runtime, data []byte, contentType string) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("size", len(data))
	_ = obj.Set("type", contentType)
	_ = obj.Set("text", func() *goja.Promise {
		return resolvedPromise(vm, vm.ToValue(string(append([]byte(nil), data...))))
	})
	_ = obj.Set("arrayBuffer", func() *goja.Promise {
		return resolvedPromise(vm, vm.ToValue(vm.NewArrayBuffer(append([]byte(nil), data...))))
	})
	_ = obj.Set("bytes", func() *goja.Promise {
		return resolvedPromise(vm, uint8ArrayValue(vm, data))
	})
	return obj
}

func newFormDataObject(vm *goja.Runtime, values map[string][]string) *goja.Object {
	state := &webFormDataState{values: values}
	obj := vm.NewObject()
	_ = obj.Set("get", func(name string) goja.Value {
		items := state.values[name]
		if len(items) == 0 {
			return goja.Null()
		}
		return vm.ToValue(items[0])
	})
	_ = obj.Set("getAll", func(name string) []string {
		return append([]string(nil), state.values[name]...)
	})
	_ = obj.Set("set", func(name, value string) {
		state.values[name] = []string{value}
	})
	_ = obj.Set("append", func(name, value string) {
		state.values[name] = append(state.values[name], value)
	})
	_ = obj.Set("has", func(name string) bool {
		_, ok := state.values[name]
		return ok
	})
	_ = obj.Set("delete", func(name string) {
		delete(state.values, name)
	})
	_ = obj.Set("entries", func() [][]string {
		var result [][]string
		for key, values := range state.values {
			for _, value := range values {
				result = append(result, []string{key, value})
			}
		}
		return result
	})
	_ = obj.Set("keys", func() []string {
		keys := make([]string, 0, len(state.values))
		for key := range state.values {
			keys = append(keys, key)
		}
		return keys
	})
	_ = obj.Set("values", func() []string {
		var result []string
		for _, values := range state.values {
			result = append(result, values...)
		}
		return result
	})
	_ = obj.Set("forEach", func(fn goja.Callable) {
		for key, values := range state.values {
			for _, value := range values {
				_, _ = fn(goja.Undefined(), vm.ToValue(value), vm.ToValue(key), obj)
			}
		}
	})
	return obj
}

func parseFormData(contentType string, data []byte) (map[string][]string, error) {
	result := map[string][]string{}
	if len(data) == 0 {
		return result, nil
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil && strings.TrimSpace(contentType) != "" {
		return nil, err
	}
	switch {
	case mediaType == "application/x-www-form-urlencoded" || contentType == "" || mediaType == "":
		values, err := nurl.ParseQuery(string(data))
		if err != nil {
			return nil, err
		}
		for key, items := range values {
			result[key] = append([]string(nil), items...)
		}
		return result, nil
	case mediaType == "multipart/form-data":
		reader := multipart.NewReader(bytes.NewReader(data), params["boundary"])
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			name := part.FormName()
			if name == "" {
				continue
			}
			payload, err := io.ReadAll(part)
			if err != nil {
				return nil, err
			}
			result[name] = append(result[name], string(payload))
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported form data content-type: %s", contentType)
	}
}

func bodyBytesFromValue(vm *goja.Runtime, value goja.Value) ([]byte, error) {
	if isNilish(value) {
		return nil, nil
	}
	switch current := value.Export().(type) {
	case nil:
		return nil, nil
	case string:
		return []byte(current), nil
	case []byte:
		return append([]byte(nil), current...), nil
	case goja.ArrayBuffer:
		return append([]byte(nil), current.Bytes()...), nil
	default:
		if bs, ok := arrayBufferViewBytes(vm, value); ok {
			return bs, nil
		}
		return nil, fmt.Errorf("unsupported body type: %T", current)
	}
}

func uint8ArrayValue(vm *goja.Runtime, data []byte) goja.Value {
	buffer := vm.ToValue(vm.NewArrayBuffer(append([]byte(nil), data...)))
	ctorValue := vm.Get("Uint8Array")
	if ctorValue == nil || goja.IsUndefined(ctorValue) || goja.IsNull(ctorValue) {
		return buffer
	}
	ctor, ok := goja.AssertFunction(ctorValue)
	if !ok {
		return buffer
	}
	value, err := ctor(goja.Undefined(), buffer)
	if err != nil {
		return buffer
	}
	return value
}
