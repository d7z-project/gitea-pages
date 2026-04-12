package goja

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"strings"

	"github.com/dop251/goja"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

const internalRequestKey = "__page_internal_request__"

type webRequestState struct {
	method   string
	url      string
	remoteIP string
	headers  http.Header
	body     []byte
	used     bool
	signal   *goja.Object
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
	bodyState := newBodyState(state.body, state.headers, &state.used)
	_ = obj.Set(internalRequestKey, state)
	_ = obj.Set("method", state.method)
	_ = obj.Set("url", state.url)
	_ = obj.Set("headers", newHeadersObject(vm, &webHeadersState{values: state.headers}))
	_ = obj.Set("signal", state.signal)
	_ = obj.Set("ip", state.remoteIP)
	_ = obj.Set("RemoteIP", state.remoteIP)
	attachBodyMethods(vm, obj, bodyState)
	_ = obj.Set("clone", func() *goja.Object {
		return newRequestObject(vm, &webRequestState{
			method:   state.method,
			url:      state.url,
			remoteIP: state.remoteIP,
			headers:  cloneHeaderValues(state.headers),
			body:     append([]byte(nil), state.body...),
			signal:   state.signal,
		})
	})
	return obj
}

func newDefaultRequestState(vm *goja.Runtime) *webRequestState {
	return &webRequestState{
		method:  http.MethodGet,
		headers: make(http.Header),
		signal:  newAbortSignalObject(vm),
	}
}

func cloneRequestState(current *webRequestState) *webRequestState {
	if current == nil {
		return nil
	}
	return &webRequestState{
		method:   current.method,
		url:      current.url,
		remoteIP: current.remoteIP,
		headers:  cloneHeaderValues(current.headers),
		body:     append([]byte(nil), current.body...),
		signal:   current.signal,
	}
}

func requestStateFromInput(vm *goja.Runtime, input goja.Value) *webRequestState {
	if current, ok := requestStateFromValue(vm, input); ok {
		state := cloneRequestState(current)
		state.used = false
		return state
	}

	state := newDefaultRequestState(vm)
	state.url = input.String()
	return state
}

func applyRequestInit(vm *goja.Runtime, state *webRequestState, init goja.Value) error {
	if state == nil || isNilish(init) {
		return nil
	}

	initObj, ok := valueObject(vm, init)
	if !ok {
		return nil
	}

	if method, ok := objectString(initObj, "method"); ok {
		state.method = strings.ToUpper(method)
	}
	if value, ok := objectValue(initObj, "headers"); ok {
		state.headers = headersFromValue(vm, value)
	}
	if value, ok := objectValue(initObj, "body"); ok {
		body, err := bodyBytesFromValue(vm, value)
		if err != nil {
			return err
		}
		state.body = body
	}
	if value, ok := objectValue(initObj, "signal"); ok {
		if signal, ok := valueObject(vm, value); ok {
			state.signal = signal
		}
	}

	return nil
}

func requestStateFromConstructor(vm *goja.Runtime, call goja.ConstructorCall) (*webRequestState, error) {
	if len(call.Arguments) == 0 {
		return nil, errors.New("request requires input")
	}

	state := requestStateFromInput(vm, call.Arguments[0])

	if len(call.Arguments) > 1 {
		if err := applyRequestInit(vm, state, call.Arguments[1]); err != nil {
			return nil, err
		}
	}
	if state.url == "" {
		return nil, errors.New("request url is required")
	}
	return state, nil
}

func requestStateFromValue(vm *goja.Runtime, value goja.Value) (*webRequestState, bool) {
	exported, ok := internalExportedValue(vm, value, internalRequestKey)
	if !ok {
		return nil, false
	}
	state, ok := exported.(*webRequestState)
	return state, ok
}

func bodyBytesFromValue(vm *goja.Runtime, value goja.Value) ([]byte, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
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
		if bs, ok := uint8ArrayBytes(vm, value); ok {
			return bs, nil
		}
		return nil, fmt.Errorf("unsupported body type: %T", current)
	}
}

func newIncomingRequestObject(vm *goja.Runtime, req *http.Request, maxBodyBytes int64) (*goja.Object, error) {
	var reader io.Reader = bytes.NewReader(nil)
	if req.Body != nil {
		reader = req.Body
	}
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
		method:   req.Method,
		url:      absoluteRequestURL(req),
		remoteIP: utils.GetRemoteIP(req),
		headers:  cloneHeaderValues(req.Header),
		body:     body,
		signal:   newAbortSignalObject(vm),
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
