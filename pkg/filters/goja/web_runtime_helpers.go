package goja

import (
	"github.com/dop251/goja"
)

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
	obj := value.ToObject(vm)
	if obj == nil {
		return nil, false
	}
	if bufferValue := obj.Get("buffer"); !goja.IsUndefined(bufferValue) && !goja.IsNull(bufferValue) {
		if buffer, ok := bufferValue.Export().(goja.ArrayBuffer); ok {
			return append([]byte(nil), buffer.Bytes()...), true
		}
	}
	return nil, false
}
