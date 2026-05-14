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

	"github.com/dop251/goja"
)

type webBodyState struct {
	data        []byte
	contentType string
	used        *bool
}

type webBodyReaderState struct {
	body     *webBodyState
	consumed bool
}

func newBodyState(data []byte, headers http.Header, used *bool) *webBodyState {
	contentType := ""
	if headers != nil {
		contentType = headers.Get("Content-Type")
	}
	return &webBodyState{
		data:        append([]byte(nil), data...),
		contentType: contentType,
		used:        used,
	}
}

func newBodyObject(vm *goja.Runtime, state *webBodyState) goja.Value {
	if state == nil || len(state.data) == 0 {
		return goja.Null()
	}
	obj := vm.NewObject()
	_ = obj.Set("getReader", func() *goja.Object {
		return newBodyReaderObject(vm, &webBodyReaderState{body: state})
	})
	return obj
}

func attachBodyMethods(vm *goja.Runtime, obj *goja.Object, state *webBodyState) {
	if obj == nil || state == nil {
		return
	}
	bodyPromise := func(transform func([]byte) (any, error)) *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		data, err := consumeWebBody(state)
		if err != nil {
			_ = reject(err)
			return promise
		}
		value, err := transform(data)
		if err != nil {
			_ = reject(err)
			return promise
		}
		_ = resolve(vm.ToValue(value))
		return promise
	}
	_ = obj.Set("body", newBodyObject(vm, state))
	_ = obj.DefineAccessorProperty("bodyUsed", vm.ToValue(func() bool {
		return state.used != nil && *state.used
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.Set("text", func() *goja.Promise {
		return bodyPromise(func(data []byte) (any, error) {
			return string(data), nil
		})
	})
	_ = obj.Set("json", func() *goja.Promise {
		return bodyPromise(func(data []byte) (any, error) {
			var decoded any
			if err := json.Unmarshal(data, &decoded); err != nil {
				return nil, err
			}
			return decoded, nil
		})
	})
	_ = obj.Set("arrayBuffer", func() *goja.Promise {
		return bodyPromise(func(data []byte) (any, error) {
			return vm.NewArrayBuffer(data), nil
		})
	})
	_ = obj.Set("bytes", func() *goja.Promise {
		return bodyPromise(func(data []byte) (any, error) {
			return uint8ArrayValue(vm, data), nil
		})
	})
	_ = obj.Set("blob", func() *goja.Promise {
		return bodyPromise(func(data []byte) (any, error) {
			return newBlobObject(vm, data, state.contentType), nil
		})
	})
	_ = obj.Set("formData", func() *goja.Promise {
		return bodyPromise(func(data []byte) (any, error) {
			form, err := parseFormData(state.contentType, data)
			if err != nil {
				return nil, err
			}
			return newFormDataObject(vm, form), nil
		})
	})
}

func newBodyReaderObject(vm *goja.Runtime, state *webBodyReaderState) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("read", func() *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		if state.consumed {
			_ = resolve(vm.ToValue(map[string]any{
				"done":  true,
				"value": goja.Undefined(),
			}))
			return promise
		}
		data, err := consumeWebBody(state.body)
		if err != nil {
			_ = reject(err)
			return promise
		}
		state.consumed = true
		_ = resolve(vm.ToValue(map[string]any{
			"done":  false,
			"value": uint8ArrayValue(vm, data),
		}))
		return promise
	})
	return obj
}

func consumeWebBody(state *webBodyState) ([]byte, error) {
	if state == nil {
		return nil, nil
	}
	if state.used != nil && *state.used {
		return nil, errors.New("body stream already read")
	}
	if state.used != nil {
		*state.used = true
	}
	return append([]byte(nil), state.data...), nil
}

func newBlobObject(vm *goja.Runtime, data []byte, contentType string) *goja.Object {
	obj := vm.NewObject()
	resolve := func(value any) *goja.Promise {
		promise, doResolve, _ := vm.NewPromise()
		_ = doResolve(vm.ToValue(value))
		return promise
	}
	_ = obj.Set("size", len(data))
	_ = obj.Set("type", contentType)
	_ = obj.Set("text", func() *goja.Promise {
		return resolve(string(append([]byte(nil), data...)))
	})
	_ = obj.Set("arrayBuffer", func() *goja.Promise {
		return resolve(vm.NewArrayBuffer(append([]byte(nil), data...)))
	})
	_ = obj.Set("bytes", func() *goja.Promise {
		return resolve(uint8ArrayValue(vm, data))
	})
	return obj
}

type webFormDataState struct {
	values map[string][]string
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
