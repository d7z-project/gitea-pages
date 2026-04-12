package goja

import (
	"encoding/json"
	"errors"
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
		exported := any(nil)
		if data != nil && !goja.IsUndefined(data) && !goja.IsNull(data) {
			exported = data.Export()
		}
		body, err := json.Marshal(exported)
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
	if state.statusText == "" {
		state.statusText = http.StatusText(state.status)
	}
	obj := vm.NewObject()
	bodyState := newBodyState(state.body, state.headers, &state.used)
	_ = obj.Set(internalResponseKey, state)
	_ = obj.Set("status", state.status)
	_ = obj.Set("statusText", state.statusText)
	_ = obj.Set("headers", newHeadersObject(vm, &webHeadersState{values: state.headers}))
	_ = obj.DefineAccessorProperty("ok", vm.ToValue(func() bool {
		return state.status >= 200 && state.status < 300
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	attachBodyMethods(vm, obj, bodyState)
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
	if len(call.Arguments) > 0 && call.Arguments[0] != nil && !goja.IsUndefined(call.Arguments[0]) && !goja.IsNull(call.Arguments[0]) {
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
	exported, ok := internalExportedValue(vm, value, internalResponseKey)
	if !ok {
		return nil, false
	}
	state, ok := exported.(*webResponseState)
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
