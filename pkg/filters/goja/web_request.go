package goja

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"strings"
	"sync/atomic"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

const internalRequestKey = "__page_internal_request__"

type webRequestState struct {
	method   string
	url      string
	remoteIP string
	headers  http.Header
	body     bodySource
	used     atomic.Bool
	signal   *goja.Object
	abort    *abortSignalState
}

func installRequest(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState) error {
	ctor := func(call goja.ConstructorCall) *goja.Object {
		state, err := requestStateFromConstructor(vm, call)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return newRequestObject(vm, loop, runtime, state)
	}
	return vm.Set("Request", vm.ToValue(ctor))
}

func newRequestObject(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, state *webRequestState) *goja.Object {
	obj := vm.NewObject()
	bodyState := newBodyState(state.headers, &state.used, state.body, loop, runtime)
	_ = obj.Set(internalRequestKey, state)
	_ = obj.Set("method", state.method)
	_ = obj.Set("url", state.url)
	_ = obj.Set("headers", newHeadersObject(vm, &webHeadersState{values: state.headers}))
	_ = obj.Set("signal", state.signal)
	_ = obj.Set("ip", state.remoteIP)
	_ = obj.Set("RemoteIP", state.remoteIP)
	attachBodyMethods(vm, obj, bodyState)
	_ = obj.Set("clone", func() (*goja.Object, error) {
		cloned, err := cloneRequestState(state)
		if err != nil {
			return nil, err
		}
		return newRequestObject(vm, loop, runtime, cloned), nil
	})
	return obj
}

func newDefaultRequestState(vm *goja.Runtime) *webRequestState {
	signal, abort := newAbortSignal(vm)
	return &webRequestState{
		method:  http.MethodGet,
		headers: make(http.Header),
		signal:  signal,
		abort:   abort,
	}
}

func cloneRequestState(current *webRequestState) (*webRequestState, error) {
	if current == nil {
		return nil, nil
	}
	var clonedBody bodySource
	var err error
	if current.body != nil {
		clonedBody, err = current.body.clone()
		if err != nil {
			return nil, err
		}
	}
	return &webRequestState{
		method:   current.method,
		url:      current.url,
		remoteIP: current.remoteIP,
		headers:  cloneHeaderValues(current.headers),
		body:     clonedBody,
		signal:   current.signal,
		abort:    current.abort,
	}, nil
}

func requestStateFromInput(vm *goja.Runtime, input goja.Value) (*webRequestState, error) {
	if current, ok := requestStateFromValue(vm, input); ok {
		state, err := cloneRequestState(current)
		if err != nil {
			return nil, err
		}
		return state, nil
	}

	state := newDefaultRequestState(vm)
	state.url = input.String()
	return state, nil
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
		state.body = newBufferedBodySource(body)
	}
	if value, ok := objectValue(initObj, "signal"); ok {
		signal, abort, err := abortSignalFromValue(vm, value)
		if err != nil {
			return err
		}
		state.signal = signal
		state.abort = abort
	}

	return nil
}

func requestStateFromConstructor(vm *goja.Runtime, call goja.ConstructorCall) (*webRequestState, error) {
	if len(call.Arguments) == 0 {
		return nil, errors.New("request requires input")
	}

	state, err := requestStateFromInput(vm, call.Arguments[0])
	if err != nil {
		return nil, err
	}
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
	obj, ok := valueObject(vm, value)
	if !ok {
		return nil, false
	}
	internal, ok := objectValue(obj, internalRequestKey)
	if !ok {
		return nil, false
	}
	state, ok := internal.Export().(*webRequestState)
	return state, ok
}

func newIncomingRequestObject(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, req *http.Request, maxBodyBytes int64, closers *Closers) (*goja.Object, error) {
	info := core.RequestInfoFromRequest(req)
	signal, abort := newAbortSignal(vm)
	var source bodySource
	if req.Body != nil {
		closers.AddCloser(req.Body.Close)
		var (
			read int64
			used bool
		)
		source = newStreamingBodySource(func() (io.ReadCloser, error) {
			if used {
				return nil, errors.New("body stream already read")
			}
			used = true
			reader := req.Body
			if maxBodyBytes <= 0 {
				return reader, nil
			}
			return newLimitedReadCloser(reader, func(n int) error {
				read += int64(n)
				if read > maxBodyBytes {
					return fmt.Errorf("request body exceeds limit: %d", maxBodyBytes)
				}
				return nil
			}), nil
		})
	}
	requestObj := newRequestObject(vm, loop, runtime, &webRequestState{
		method:   req.Method,
		url:      absoluteRequestURL(req, info),
		remoteIP: info.ClientIP,
		headers:  cloneHeaderValues(req.Header),
		body:     source,
		signal:   signal,
		abort:    abort,
	})
	return requestObj, nil
}

func absoluteRequestURL(req *http.Request, info core.RequestInfo) string {
	if req == nil || req.URL == nil {
		return ""
	}
	if req.URL.IsAbs() {
		return req.URL.String()
	}
	cloned := new(nurl.URL)
	*cloned = *req.URL
	cloned.Scheme = info.Scheme
	cloned.Host = req.Host
	return cloned.String()
}
