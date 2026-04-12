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
	_ = obj.Set("body", newBodyObject(vm, state))
	_ = obj.DefineAccessorProperty("bodyUsed", vm.ToValue(func() bool {
		return state.used != nil && *state.used
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.Set("text", func() *goja.Promise {
		return bodyTextPromise(vm, state)
	})
	_ = obj.Set("json", func() *goja.Promise {
		return bodyJSONPromise(vm, state)
	})
	_ = obj.Set("arrayBuffer", func() *goja.Promise {
		return bodyArrayBufferPromise(vm, state)
	})
	_ = obj.Set("bytes", func() *goja.Promise {
		return bodyBytesPromise(vm, state)
	})
	_ = obj.Set("blob", func() *goja.Promise {
		return bodyBlobPromise(vm, state)
	})
	_ = obj.Set("formData", func() *goja.Promise {
		return bodyFormDataPromise(vm, state)
	})
}

func newBodyReaderObject(vm *goja.Runtime, state *webBodyReaderState) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("read", func() *goja.Promise {
		if state.consumed {
			return resolvedPromise(vm, map[string]any{
				"done":  true,
				"value": goja.Undefined(),
			})
		}
		data, err := consumeWebBody(state.body)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		state.consumed = true
		return resolvedPromise(vm, map[string]any{
			"done":  false,
			"value": uint8ArrayValue(vm, data),
		})
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

func bodyTextPromise(vm *goja.Runtime, state *webBodyState) *goja.Promise {
	data, err := consumeWebBody(state)
	if err != nil {
		return rejectedPromise(vm, err)
	}
	return resolvedPromise(vm, string(data))
}

func bodyJSONPromise(vm *goja.Runtime, state *webBodyState) *goja.Promise {
	data, err := consumeWebBody(state)
	if err != nil {
		return rejectedPromise(vm, err)
	}
	var decoded any
	if err = json.Unmarshal(data, &decoded); err != nil {
		return rejectedPromise(vm, err)
	}
	return resolvedPromise(vm, decoded)
}

func bodyArrayBufferPromise(vm *goja.Runtime, state *webBodyState) *goja.Promise {
	data, err := consumeWebBody(state)
	if err != nil {
		return rejectedPromise(vm, err)
	}
	return resolvedPromise(vm, vm.NewArrayBuffer(data))
}

func bodyBytesPromise(vm *goja.Runtime, state *webBodyState) *goja.Promise {
	data, err := consumeWebBody(state)
	if err != nil {
		return rejectedPromise(vm, err)
	}
	return resolvedPromise(vm, uint8ArrayValue(vm, data))
}

func bodyBlobPromise(vm *goja.Runtime, state *webBodyState) *goja.Promise {
	data, err := consumeWebBody(state)
	if err != nil {
		return rejectedPromise(vm, err)
	}
	return resolvedPromise(vm, newBlobObject(vm, data, state.contentType))
}

func bodyFormDataPromise(vm *goja.Runtime, state *webBodyState) *goja.Promise {
	data, err := consumeWebBody(state)
	if err != nil {
		return rejectedPromise(vm, err)
	}
	form, err := parseFormData(state.contentType, data)
	if err != nil {
		return rejectedPromise(vm, err)
	}
	return resolvedPromise(vm, newFormDataObject(vm, form))
}

func newBlobObject(vm *goja.Runtime, data []byte, contentType string) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("size", len(data))
	_ = obj.Set("type", contentType)
	_ = obj.Set("text", func() *goja.Promise {
		return resolvedPromise(vm, string(append([]byte(nil), data...)))
	})
	_ = obj.Set("arrayBuffer", func() *goja.Promise {
		return resolvedPromise(vm, vm.NewArrayBuffer(append([]byte(nil), data...)))
	})
	_ = obj.Set("bytes", func() *goja.Promise {
		return resolvedPromise(vm, uint8ArrayValue(vm, data))
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
