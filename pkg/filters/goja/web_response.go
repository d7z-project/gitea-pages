package goja

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	"github.com/dop251/goja"
)

const internalResponseKey = "__page_internal_response__"

type webResponseState struct {
	status     int
	statusText string
	headers    http.Header
	body       []byte
	used       bool
	upgrade    func() error
}

func installResponse(vm *goja.Runtime) error {
	ctor := func(call goja.ConstructorCall) *goja.Object {
		state, err := responseStateFromConstructor(vm, call)
		if err != nil {
			panic(err)
		}
		return newResponseObject(vm, state)
	}
	fn := vm.ToValue(ctor)
	obj := fn.ToObject(vm)
	_ = obj.Set("json", func(data goja.Value, init ...goja.Value) *goja.Object {
		body, err := json.Marshal(data.Export())
		if err != nil {
			panic(err)
		}
		state := &webResponseState{
			status:  http.StatusOK,
			headers: make(http.Header),
			body:    body,
		}
		state.headers.Set("Content-Type", "application/json")
		if len(init) > 0 {
			applyResponseInit(vm, state, init[0])
		}
		return newResponseObject(vm, state)
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
		return newResponseObject(vm, state)
	})
	return vm.Set("Response", fn)
}

func newResponseObject(vm *goja.Runtime, state *webResponseState) *goja.Object {
	if state.status == 0 {
		state.status = http.StatusOK
	}
	obj := vm.NewObject()
	_ = obj.Set(internalResponseKey, state)
	_ = obj.Set("status", state.status)
	_ = obj.Set("statusText", state.statusText)
	_ = obj.Set("headers", newHeadersObject(vm, &webHeadersState{values: state.headers}))
	_ = obj.DefineAccessorProperty("ok", vm.ToValue(func() bool {
		return state.status >= 200 && state.status < 300
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.DefineAccessorProperty("bodyUsed", vm.ToValue(func() bool {
		return state.used
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.Set("text", func() *goja.Promise {
		state.used = true
		return resolvedPromise(vm, string(state.body))
	})
	_ = obj.Set("json", func() *goja.Promise {
		state.used = true
		var decoded any
		if err := json.Unmarshal(state.body, &decoded); err != nil {
			return rejectedPromise(vm, err)
		}
		return resolvedPromise(vm, decoded)
	})
	_ = obj.Set("arrayBuffer", func() *goja.Promise {
		state.used = true
		return resolvedPromise(vm, vm.NewArrayBuffer(append([]byte(nil), state.body...)))
	})
	_ = obj.Set("clone", func() *goja.Object {
		return newResponseObject(vm, &webResponseState{
			status:     state.status,
			statusText: state.statusText,
			headers:    cloneHeaderValues(state.headers),
			body:       append([]byte(nil), state.body...),
		})
	})
	return obj
}

func responseStateFromConstructor(vm *goja.Runtime, call goja.ConstructorCall) (*webResponseState, error) {
	state := &webResponseState{
		status:  http.StatusOK,
		headers: make(http.Header),
	}
	if len(call.Arguments) > 0 && !goja.IsUndefined(call.Arguments[0]) && !goja.IsNull(call.Arguments[0]) {
		body, err := bodyBytesFromValue(vm, call.Arguments[0])
		if err != nil {
			return nil, err
		}
		state.body = body
	}
	if len(call.Arguments) > 1 {
		applyResponseInit(vm, state, call.Arguments[1])
	}
	return state, nil
}

func applyResponseInit(vm *goja.Runtime, state *webResponseState, value goja.Value) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return
	}
	obj := value.ToObject(vm)
	if obj == nil {
		return
	}
	if next := obj.Get("status"); next != nil && !goja.IsUndefined(next) && !goja.IsNull(next) {
		state.status = int(next.ToInteger())
	}
	if next := obj.Get("statusText"); next != nil && !goja.IsUndefined(next) && !goja.IsNull(next) {
		state.statusText = next.String()
	}
	if next := obj.Get("headers"); next != nil && !goja.IsUndefined(next) && !goja.IsNull(next) {
		state.headers = headersFromValue(vm, next)
	}
}

func responseStateFromValue(vm *goja.Runtime, value goja.Value) (*webResponseState, bool) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, false
	}
	obj := value.ToObject(vm)
	if obj == nil {
		return nil, false
	}
	internal := obj.Get(internalResponseKey)
	if internal == nil || goja.IsUndefined(internal) || goja.IsNull(internal) {
		return nil, false
	}
	exported := internal.Export()
	state, ok := exported.(*webResponseState)
	return state, ok
}

func writeResponseValue(vm *goja.Runtime, writer http.ResponseWriter, value goja.Value) error {
	state, ok := responseStateFromValue(vm, value)
	if !ok {
		return fmt.Errorf("handler must return Response, got %s", reflect.TypeOf(value.Export()))
	}
	for key, values := range state.headers {
		for _, item := range values {
			writer.Header().Add(key, item)
		}
	}
	writer.WriteHeader(state.status)
	if len(state.body) == 0 {
		return nil
	}
	_, err := writer.Write(state.body)
	return err
}

func upgradeResponseValue(vm *goja.Runtime, value goja.Value) (func() error, bool) {
	state, ok := responseStateFromValue(vm, value)
	if !ok || state.upgrade == nil {
		return nil, false
	}
	return state.upgrade, true
}
