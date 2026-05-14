package goja

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/dop251/goja"
)

const internalAbortSignalKey = "__page_internal_abort_signal__"

var errInvalidAbortSignal = errors.New("invalid abort signal")

type abortSignalState struct {
	aborted atomic.Bool
	done    chan struct{}
	once    sync.Once
}

func newAbortSignalState() *abortSignalState {
	return &abortSignalState{done: make(chan struct{})}
}

func (s *abortSignalState) Abort() {
	if s == nil {
		return
	}
	s.aborted.Store(true)
	s.once.Do(func() {
		close(s.done)
	})
}

func (s *abortSignalState) Aborted() bool {
	return s != nil && s.aborted.Load()
}

func (s *abortSignalState) Done() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.done
}

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
		signal, state := newAbortSignal(vm)
		obj := vm.NewObject()
		_ = obj.Set("signal", signal)
		_ = obj.Set("abort", func() {
			state.Abort()
		})
		return obj
	}
	if err := vm.Set("AbortController", vm.ToValue(controller)); err != nil {
		return err
	}
	return vm.Set("AbortSignal", vm.ToValue(func(call goja.ConstructorCall) *goja.Object {
		signal, _ := newAbortSignal(vm)
		return signal
	}))
}

func newAbortSignal(vm *goja.Runtime) (*goja.Object, *abortSignalState) {
	state := newAbortSignalState()
	obj := vm.NewObject()
	_ = obj.Set(internalAbortSignalKey, state)
	_ = obj.DefineAccessorProperty("aborted",
		vm.ToValue(func() bool {
			return state.Aborted()
		}),
		vm.ToValue(func(value goja.Value) {
			if value != nil && value.ToBoolean() {
				state.Abort()
			}
		}),
		goja.FLAG_FALSE,
		goja.FLAG_TRUE,
	)
	return obj, state
}

func abortSignalFromValue(vm *goja.Runtime, value goja.Value) (*goja.Object, *abortSignalState, error) {
	obj, ok := valueObject(vm, value)
	if !ok {
		return nil, nil, errInvalidAbortSignal
	}
	internal, ok := objectValue(obj, internalAbortSignalKey)
	if !ok {
		return nil, nil, errInvalidAbortSignal
	}
	state, ok := internal.Export().(*abortSignalState)
	if !ok {
		return nil, nil, errInvalidAbortSignal
	}
	return obj, state, nil
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
