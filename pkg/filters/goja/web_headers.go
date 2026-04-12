package goja

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"
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
	exported, ok := internalExportedValue(vm, value, internalHeadersKey)
	if !ok {
		return nil, false
	}
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
	if isNilish(value) {
		return
	}
	if isArrayValue(value) {
		arr, ok := valueObject(vm, value)
		if !ok {
			return
		}
		lengthValue, ok := objectValue(arr, "length")
		if !ok {
			return
		}
		length := int(lengthValue.ToInteger())
		for i := 0; i < length; i++ {
			entry := arr.Get(strconv.Itoa(i))
			if entry == nil || goja.IsUndefined(entry) || goja.IsNull(entry) {
				continue
			}
			entryObj := entry.ToObject(vm)
			if entryObj == nil {
				continue
			}
			entryLength, ok := objectValue(entryObj, "length")
			if !isArrayValue(entry) || !ok || int(entryLength.ToInteger()) < 2 {
				continue
			}
			nameValue, ok := objectValue(entryObj, "0")
			if !ok {
				continue
			}
			name := nameValue.String()
			item := entryObj.Get("1")
			if item == nil || goja.IsUndefined(item) || goja.IsNull(item) {
				target.Add(name, "")
				continue
			}
			switch exported := item.Export().(type) {
			case []any:
				for _, v := range exported {
					target.Add(name, fmt.Sprint(v))
				}
			case []string:
				for _, v := range exported {
					target.Add(name, v)
				}
			default:
				target.Add(name, item.String())
			}
		}
		return
	}
	obj, ok := valueObject(vm, value)
	if !ok {
		return
	}
	for _, key := range obj.Keys() {
		currentValue := obj.Get(key)
		if currentValue == nil || goja.IsUndefined(currentValue) || goja.IsNull(currentValue) {
			target.Set(key, "")
			continue
		}
		exported := currentValue.Export()
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

func isArrayValue(value goja.Value) bool {
	if value == nil {
		return false
	}
	typ := reflect.TypeOf(value.Export())
	if typ == nil {
		return false
	}
	kind := typ.Kind()
	return kind == reflect.Slice || kind == reflect.Array
}
