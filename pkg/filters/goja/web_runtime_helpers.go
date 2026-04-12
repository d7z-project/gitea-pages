package goja

import (
	"github.com/dop251/goja"
)

func isNilish(value goja.Value) bool {
	return value == nil || goja.IsUndefined(value) || goja.IsNull(value)
}

func valueObject(vm *goja.Runtime, value goja.Value) (*goja.Object, bool) {
	if isNilish(value) {
		return nil, false
	}
	obj := value.ToObject(vm)
	if obj == nil {
		return nil, false
	}
	return obj, true
}

func objectValue(obj *goja.Object, key string) (goja.Value, bool) {
	if obj == nil {
		return nil, false
	}
	value := obj.Get(key)
	if isNilish(value) {
		return nil, false
	}
	return value, true
}

func objectString(obj *goja.Object, key string) (string, bool) {
	value, ok := objectValue(obj, key)
	if !ok {
		return "", false
	}
	return value.String(), true
}

func objectInt64(obj *goja.Object, key string) (int64, bool) {
	value, ok := objectValue(obj, key)
	if !ok {
		return 0, false
	}
	return value.ToInteger(), true
}

func internalExportedValue(vm *goja.Runtime, value goja.Value, key string) (any, bool) {
	obj, ok := valueObject(vm, value)
	if !ok {
		return nil, false
	}
	internal, ok := objectValue(obj, key)
	if !ok {
		return nil, false
	}
	return internal.Export(), true
}

func resolvedPromise(vm *goja.Runtime, value any) *goja.Promise {
	promise, resolve, _ := vm.NewPromise()
	_ = resolve(vm.ToValue(value))
	return promise
}

func rejectedPromise(vm *goja.Runtime, err error) *goja.Promise {
	promise, _, reject := vm.NewPromise()
	_ = reject(vm.ToValue(err))
	return promise
}

func installTextCodecs(vm *goja.Runtime) error {
	encoder := func(call goja.ConstructorCall) *goja.Object {
		obj := vm.NewObject()
		_ = obj.Set("encode", func(input string) goja.ArrayBuffer {
			return vm.NewArrayBuffer([]byte(input))
		})
		return obj
	}
	decoder := func(call goja.ConstructorCall) *goja.Object {
		obj := vm.NewObject()
		_ = obj.Set("decode", func(input goja.Value) string {
			if bytes, ok := uint8ArrayBytes(vm, input); ok {
				return string(bytes)
			}
			if !isNilish(input) {
				if buffer, ok := input.Export().(goja.ArrayBuffer); ok {
					return string(buffer.Bytes())
				}
			}
			if isNilish(input) {
				return ""
			}
			if buffer, ok := input.Export().(goja.ArrayBuffer); ok {
				return string(buffer.Bytes())
			}
			return input.String()
		})
		return obj
	}
	if err := vm.Set("TextEncoder", vm.ToValue(encoder)); err != nil {
		return err
	}
	return vm.Set("TextDecoder", vm.ToValue(decoder))
}

func installAbortPrimitives(vm *goja.Runtime) error {
	controller := func(call goja.ConstructorCall) *goja.Object {
		signal := newAbortSignalObject(vm)
		obj := vm.NewObject()
		_ = obj.Set("signal", signal)
		_ = obj.Set("abort", func() {
			_ = signal.Set("aborted", true)
		})
		return obj
	}
	if err := vm.Set("AbortController", vm.ToValue(controller)); err != nil {
		return err
	}
	return vm.Set("AbortSignal", vm.ToValue(func(call goja.ConstructorCall) *goja.Object {
		return newAbortSignalObject(vm)
	}))
}

func newAbortSignalObject(vm *goja.Runtime) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("aborted", false)
	return obj
}

func uint8ArrayBytes(vm *goja.Runtime, value goja.Value) ([]byte, bool) {
	obj, ok := valueObject(vm, value)
	if !ok {
		return nil, false
	}
	if bufferValue, ok := objectValue(obj, "buffer"); ok {
		if buffer, ok := bufferValue.Export().(goja.ArrayBuffer); ok {
			return append([]byte(nil), buffer.Bytes()...), true
		}
	}
	if _, ok := objectValue(obj, "byteLength"); !ok {
		return nil, false
	}
	return nil, false
}
