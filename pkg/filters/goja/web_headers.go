package goja

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dop251/goja"
)

const internalHeadersKey = "__page_internal_headers__"

type webHeadersState struct {
	values http.Header
}

func installHeaders(vm *goja.Runtime) error {
	ctor := func(call goja.ConstructorCall) *goja.Object {
		state := &webHeadersState{values: make(http.Header)}
		if len(call.Arguments) > 0 {
			mergeHeadersValue(vm, state.values, call.Arguments[0])
		}
		return newHeadersObject(vm, state)
	}
	fn := vm.ToValue(ctor)
	return vm.Set("Headers", fn)
}

func newHeadersObject(vm *goja.Runtime, state *webHeadersState) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set(internalHeadersKey, state)
	_ = obj.Set("get", func(name string) goja.Value {
		value := state.values.Get(name)
		if value == "" {
			return goja.Null()
		}
		return vm.ToValue(value)
	})
	_ = obj.Set("set", func(name, value string) {
		state.values.Set(name, value)
	})
	_ = obj.Set("append", func(name, value string) {
		state.values.Add(name, value)
	})
	_ = obj.Set("has", func(name string) bool {
		_, ok := state.values[http.CanonicalHeaderKey(name)]
		return ok
	})
	_ = obj.Set("delete", func(name string) {
		state.values.Del(name)
	})
	_ = obj.Set("keys", func() []string {
		keys := make([]string, 0, len(state.values))
		for key := range state.values {
			keys = append(keys, strings.ToLower(key))
		}
		return keys
	})
	_ = obj.Set("values", func() []string {
		values := make([]string, 0, len(state.values))
		for _, items := range state.values {
			values = append(values, strings.Join(items, ", "))
		}
		return values
	})
	_ = obj.Set("entries", func() [][]string {
		entries := make([][]string, 0, len(state.values))
		for key, items := range state.values {
			entries = append(entries, []string{strings.ToLower(key), strings.Join(items, ", ")})
		}
		return entries
	})
	_ = obj.Set("forEach", func(fn goja.Callable) {
		for key, items := range state.values {
			_, _ = fn(goja.Undefined(), vm.ToValue(strings.Join(items, ", ")), vm.ToValue(strings.ToLower(key)), obj)
		}
	})
	return obj
}

func headersStateFromValue(vm *goja.Runtime, value goja.Value) (*webHeadersState, bool) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, false
	}
	obj := value.ToObject(vm)
	if obj == nil {
		return nil, false
	}
	internal := obj.Get(internalHeadersKey)
	if internal == nil || goja.IsUndefined(internal) || goja.IsNull(internal) {
		return nil, false
	}
	exported := internal.Export()
	state, ok := exported.(*webHeadersState)
	return state, ok
}

func cloneHeaderValues(header http.Header) http.Header {
	cloned := make(http.Header, len(header))
	for key, values := range header {
		next := make([]string, len(values))
		copy(next, values)
		cloned[key] = next
	}
	return cloned
}

func headersFromValue(vm *goja.Runtime, value goja.Value) http.Header {
	headers := make(http.Header)
	mergeHeadersValue(vm, headers, value)
	return headers
}

func mergeHeadersValue(vm *goja.Runtime, target http.Header, value goja.Value) {
	if state, ok := headersStateFromValue(vm, value); ok {
		for key, values := range state.values {
			for _, item := range values {
				target.Add(key, item)
			}
		}
		return
	}
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return
	}
	obj := value.ToObject(vm)
	if obj == nil {
		return
	}
	for _, key := range obj.Keys() {
		exported := obj.Get(key).Export()
		switch current := exported.(type) {
		case []any:
			for _, item := range current {
				target.Add(key, fmt.Sprint(item))
			}
		case []string:
			for _, item := range current {
				target.Add(key, item)
			}
		default:
			target.Set(key, fmt.Sprint(exported))
		}
	}
}
