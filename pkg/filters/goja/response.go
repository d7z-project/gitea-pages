package goja

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

const internalResponseKey = "__page_internal_response__"

type webResponseState struct {
	status     int
	statusText string
	headers    http.Header
	body       bodySource
	used       atomic.Bool
	upgrade    func() error
	stream     *responseStreamState
}

func installResponse(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState) error {
	ctor := func(call goja.ConstructorCall) *goja.Object {
		state, err := responseStateFromConstructor(vm, call)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return newResponseObject(vm, loop, runtime, state)
	}
	fn := vm.ToValue(ctor)
	obj := fn.ToObject(vm)
	_ = obj.Set("json", func(data goja.Value, init ...goja.Value) *goja.Object {
		exported := any(nil)
		if data != nil && !goja.IsUndefined(data) && !goja.IsNull(data) {
			exported = data.Export()
		}
		body, err := json.Marshal(exported)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		state := &webResponseState{
			status:  http.StatusOK,
			headers: make(http.Header),
			body:    newBufferedBodySource(body),
		}
		state.headers.Set("Content-Type", "application/json")
		if len(init) > 0 {
			applyResponseInit(vm, state, init[0])
		}
		return newResponseObject(vm, loop, runtime, state)
	})
	_ = obj.Set("redirect", func(location string, status ...int) *goja.Object {
		code := http.StatusFound
		if len(status) > 0 {
			code = status[0]
		}
		state := &webResponseState{
			status:  code,
			headers: make(http.Header),
		}
		state.headers.Set("Location", location)
		return newResponseObject(vm, loop, runtime, state)
	})
	return vm.Set("Response", fn)
}

func newResponseObject(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, state *webResponseState) *goja.Object {
	if state.status == 0 {
		state.status = http.StatusOK
	}
	if state.statusText == "" {
		state.statusText = http.StatusText(state.status)
	}
	obj := vm.NewObject()
	bodyState := newBodyState(state.headers, &state.used, state.body, loop, runtime)
	_ = obj.Set(internalResponseKey, state)
	_ = obj.Set("status", state.status)
	_ = obj.Set("statusText", state.statusText)
	_ = obj.Set("headers", newHeadersObject(vm, &webHeadersState{values: state.headers}))
	_ = obj.DefineAccessorProperty("ok", vm.ToValue(func() bool {
		return state.status >= 200 && state.status < 300
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	attachBodyMethods(vm, obj, bodyState)
	_ = obj.Set("clone", func() (*goja.Object, error) {
		cloned, err := cloneResponseState(state)
		if err != nil {
			return nil, err
		}
		return newResponseObject(vm, loop, runtime, cloned), nil
	})
	return obj
}

func cloneResponseState(current *webResponseState) (*webResponseState, error) {
	if current == nil {
		return nil, nil
	}
	if current.stream != nil {
		return nil, errors.New("response stream cannot be cloned")
	}
	var clonedBody bodySource
	var err error
	if current.body != nil {
		clonedBody, err = current.body.clone()
		if err != nil {
			return nil, err
		}
	}
	return &webResponseState{
		status:     current.status,
		statusText: current.statusText,
		headers:    cloneHeaderValues(current.headers),
		body:       clonedBody,
		upgrade:    current.upgrade,
		stream:     current.stream,
	}, nil
}

func responseStateFromConstructor(vm *goja.Runtime, call goja.ConstructorCall) (*webResponseState, error) {
	state := &webResponseState{
		status:  http.StatusOK,
		headers: make(http.Header),
	}
	if len(call.Arguments) > 0 && !isNilish(call.Arguments[0]) {
		body, err := bodyBytesFromValue(vm, call.Arguments[0])
		if err != nil {
			return nil, err
		}
		state.body = newBufferedBodySource(body)
	}
	if len(call.Arguments) > 1 {
		applyResponseInit(vm, state, call.Arguments[1])
	}
	return state, nil
}

func applyResponseInit(vm *goja.Runtime, state *webResponseState, value goja.Value) {
	if isNilish(value) {
		return
	}
	obj, ok := valueObject(vm, value)
	if !ok {
		return
	}
	if next, ok := objectValue(obj, "status"); ok {
		state.status = int(next.ToInteger())
		if state.statusText == "" {
			state.statusText = http.StatusText(state.status)
		}
	}
	if next, ok := objectValue(obj, "statusText"); ok {
		state.statusText = next.String()
	}
	if next, ok := objectValue(obj, "headers"); ok {
		state.headers = headersFromValue(vm, next)
	}
}

func responseStateFromValue(vm *goja.Runtime, value goja.Value) (*webResponseState, bool) {
	obj, ok := valueObject(vm, value)
	if !ok {
		return nil, false
	}
	internal, ok := objectValue(obj, internalResponseKey)
	if !ok {
		return nil, false
	}
	state, ok := internal.Export().(*webResponseState)
	return state, ok
}

func writeResponseValue(vm *goja.Runtime, writer http.ResponseWriter, value goja.Value) error {
	state, ok := responseStateFromValue(vm, value)
	if !ok {
		if value == nil {
			return errors.New("handler must return Response, got <nil>")
		}
		return fmt.Errorf("handler must return Response, got %s", reflect.TypeOf(value.Export()))
	}
	if state.stream != nil {
		return state.stream.serve(writer)
	}
	for key, values := range state.headers {
		for _, item := range values {
			writer.Header().Add(key, item)
		}
	}
	writer.WriteHeader(state.status)
	if state.body == nil || state.body.empty() {
		return nil
	}
	bodyState := newBodyState(state.headers, &state.used, state.body, nil, nil)
	data, err := consumeWebBody(bodyState)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	_, err = writer.Write(data)
	return err
}

func upgradeResponseValue(vm *goja.Runtime, value goja.Value) (func() error, bool) {
	state, ok := responseStateFromValue(vm, value)
	if !ok || state.upgrade == nil {
		return nil, false
	}
	return state.upgrade, true
}
