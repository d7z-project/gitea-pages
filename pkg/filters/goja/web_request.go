package goja

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"strings"

	"github.com/dop251/goja"
)

const internalRequestKey = "__page_internal_request__"

type webRequestState struct {
	method  string
	url     string
	headers http.Header
	body    []byte
	used    bool
	signal  *goja.Object
}

func installRequest(vm *goja.Runtime) error {
	ctor := func(call goja.ConstructorCall) *goja.Object {
		state, err := requestStateFromConstructor(vm, call)
		if err != nil {
			panic(err)
		}
		return newRequestObject(vm, state)
	}
	return vm.Set("Request", vm.ToValue(ctor))
}

func newRequestObject(vm *goja.Runtime, state *webRequestState) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set(internalRequestKey, state)
	_ = obj.Set("method", state.method)
	_ = obj.Set("url", state.url)
	_ = obj.Set("headers", newHeadersObject(vm, &webHeadersState{values: state.headers}))
	_ = obj.Set("signal", state.signal)
	_ = obj.DefineAccessorProperty("bodyUsed", vm.ToValue(func() bool {
		return state.used
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.Set("text", func() *goja.Promise {
		return resolvedPromise(vm, string(consumeBody(state)))
	})
	_ = obj.Set("json", func() *goja.Promise {
		data := consumeBody(state)
		var decoded any
		if err := json.Unmarshal(data, &decoded); err != nil {
			return rejectedPromise(vm, err)
		}
		return resolvedPromise(vm, decoded)
	})
	_ = obj.Set("arrayBuffer", func() *goja.Promise {
		return resolvedPromise(vm, vm.NewArrayBuffer(consumeBody(state)))
	})
	_ = obj.Set("clone", func() *goja.Object {
		return newRequestObject(vm, &webRequestState{
			method:  state.method,
			url:     state.url,
			headers: cloneHeaderValues(state.headers),
			body:    append([]byte(nil), state.body...),
			signal:  state.signal,
		})
	})
	return obj
}

func requestStateFromConstructor(vm *goja.Runtime, call goja.ConstructorCall) (*webRequestState, error) {
	if len(call.Arguments) == 0 {
		return nil, errors.New("request requires input")
	}

	state := &webRequestState{
		method:  http.MethodGet,
		headers: make(http.Header),
		body:    nil,
		signal:  newAbortSignalObject(vm),
	}

	if current, ok := requestStateFromValue(vm, call.Arguments[0]); ok {
		*state = *current
		state.headers = cloneHeaderValues(current.headers)
		state.body = append([]byte(nil), current.body...)
		state.used = false
		state.signal = current.signal
	} else {
		state.url = call.Arguments[0].String()
	}

	if len(call.Arguments) > 1 {
		initObj := call.Arguments[1].ToObject(vm)
		if initObj != nil {
			if value := initObj.Get("method"); !goja.IsUndefined(value) && !goja.IsNull(value) {
				state.method = strings.ToUpper(value.String())
			}
			if value := initObj.Get("headers"); !goja.IsUndefined(value) && !goja.IsNull(value) {
				state.headers = headersFromValue(vm, value)
			}
			if value := initObj.Get("body"); !goja.IsUndefined(value) && !goja.IsNull(value) {
				body, err := bodyBytesFromValue(vm, value)
				if err != nil {
					return nil, err
				}
				state.body = body
			}
			if value := initObj.Get("signal"); !goja.IsUndefined(value) && !goja.IsNull(value) {
				if signal := value.ToObject(vm); signal != nil {
					state.signal = signal
				}
			}
		}
	}
	if state.url == "" {
		return nil, errors.New("Request url is required")
	}
	return state, nil
}

func requestStateFromValue(vm *goja.Runtime, value goja.Value) (*webRequestState, bool) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, false
	}
	obj := value.ToObject(vm)
	if obj == nil {
		return nil, false
	}
	internal := obj.Get(internalRequestKey)
	if internal == nil || goja.IsUndefined(internal) || goja.IsNull(internal) {
		return nil, false
	}
	exported := internal.Export()
	state, ok := exported.(*webRequestState)
	return state, ok
}

func consumeBody(state *webRequestState) []byte {
	state.used = true
	return append([]byte(nil), state.body...)
}

func bodyBytesFromValue(vm *goja.Runtime, value goja.Value) ([]byte, error) {
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
		if bytes, ok := uint8ArrayBytes(vm, value); ok {
			return bytes, nil
		}
		return nil, fmt.Errorf("unsupported body type: %T", current)
	}
}

func newIncomingRequestObject(vm *goja.Runtime, req *http.Request, maxBodyBytes int64) (*goja.Object, error) {
	var reader io.Reader = req.Body
	if maxBodyBytes > 0 && req.Body != nil {
		reader = io.LimitReader(req.Body, maxBodyBytes+1)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBodyBytes > 0 && int64(len(body)) > maxBodyBytes {
		return nil, fmt.Errorf("request body exceeds limit: %d", maxBodyBytes)
	}
	req.Body = cloneBody(body)
	return newRequestObject(vm, &webRequestState{
		method:  req.Method,
		url:     absoluteRequestURL(req),
		headers: cloneHeaderValues(req.Header),
		body:    body,
		signal:  newAbortSignalObject(vm),
	}), nil
}

func absoluteRequestURL(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	if req.URL.IsAbs() {
		return req.URL.String()
	}
	cloned := new(nurl.URL)
	*cloned = *req.URL
	if proto := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")); proto != "" {
		cloned.Scheme = strings.ToLower(strings.Split(proto, ",")[0])
	} else if req.TLS != nil {
		cloned.Scheme = "https"
	} else {
		cloned.Scheme = "http"
	}
	cloned.Host = req.Host
	return cloned.String()
}
