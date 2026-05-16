package goja

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
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

func objectBool(obj *goja.Object, key string) (bool, bool) {
	value, ok := objectValue(obj, key)
	if !ok {
		return false, false
	}
	return value.ToBoolean(), true
}

func resolveHandler(vm *goja.Runtime, handler goja.Value) (goja.Callable, goja.Value, error) {
	if isNilish(handler) {
		return nil, nil, errInvalidHandler
	}
	if fn, ok := goja.AssertFunction(handler); ok {
		return fn, goja.Undefined(), nil
	}
	obj, ok := valueObject(vm, handler)
	if !ok {
		return nil, nil, errInvalidHandler
	}
	fetchValue, ok := objectValue(obj, "fetch")
	if !ok {
		return nil, nil, errInvalidHandler
	}
	fetchFn, ok := goja.AssertFunction(fetchValue)
	if !ok {
		return nil, nil, errInvalidHandler
	}
	return fetchFn, obj, nil
}

func responseIOUnavailableError(name string) error {
	return fmt.Errorf("%s is unavailable: response already committed", name)
}

func rejectedPromise(vm *goja.Runtime, err error) *goja.Promise {
	promise, _, reject := vm.NewPromise()
	_ = reject(vm.ToValue(err))
	return promise
}

func resolvedPromise(vm *goja.Runtime, value goja.Value) *goja.Promise {
	promise, resolve, _ := vm.NewPromise()
	_ = resolve(value)
	return promise
}

func syncVoidPromise(vm *goja.Runtime, work func() error) *goja.Promise {
	if err := work(); err != nil {
		return rejectedPromise(vm, err)
	}
	return resolvedPromise(vm, goja.Undefined())
}

func defineClosedAccessor(obj *goja.Object, vm *goja.Runtime, closed func() bool) {
	_ = obj.DefineAccessorProperty("closed", vm.ToValue(closed), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
}

func asyncValuePromise[T any](
	vm *goja.Runtime,
	loop *eventloop.EventLoop,
	runtime *runtimeState,
	work func() (T, error),
	toValue func(*goja.Runtime, T) (goja.Value, error),
) *goja.Promise {
	promise, resolve, reject := vm.NewPromise()
	if runtime != nil && !runtime.startTask() {
		_ = reject(vm.ToValue(errRuntimeClosing))
		return promise
	}
	go func() {
		if runtime != nil {
			defer runtime.finishTask()
		}
		result, err := work()
		settle := func(loopVM *goja.Runtime) {
			if err != nil {
				_ = reject(loopVM.ToValue(err))
				return
			}
			value, convertErr := toValue(loopVM, result)
			if convertErr != nil {
				_ = reject(loopVM.ToValue(convertErr))
				return
			}
			_ = resolve(value)
		}
		if loop == nil {
			settle(vm)
			return
		}
		if runtime != nil {
			runtime.runOnLoop(loop, settle)
			return
		}
		loop.RunOnLoop(settle)
	}()
	return promise
}

func asyncVoidPromise(
	vm *goja.Runtime,
	loop *eventloop.EventLoop,
	runtime *runtimeState,
	work func() error,
) *goja.Promise {
	return asyncValuePromise(vm, loop, runtime, func() (struct{}, error) {
		return struct{}{}, work()
	}, func(*goja.Runtime, struct{}) (goja.Value, error) {
		return goja.Undefined(), nil
	})
}

func writtenResponseFunc(writer http.ResponseWriter) func() bool {
	return func() bool {
		return utils.IsWrittenResponseWriter(writer)
	}
}

func safeHTTPFlush(flusher http.Flusher) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("http flush panic: %v", r)
		}
	}()
	flusher.Flush()
	return nil
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
			if bytes, ok := arrayBufferViewBytes(vm, input); ok {
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

func arrayBufferViewBytes(vm *goja.Runtime, value goja.Value) ([]byte, bool) {
	obj, ok := valueObject(vm, value)
	if !ok {
		return nil, false
	}
	bufferValue, ok := objectValue(obj, "buffer")
	if !ok {
		return nil, false
	}
	buffer, ok := bufferValue.Export().(goja.ArrayBuffer)
	if !ok {
		return nil, false
	}
	offsetValue, ok := objectValue(obj, "byteOffset")
	if !ok {
		return nil, false
	}
	lengthValue, ok := objectValue(obj, "byteLength")
	if !ok {
		return nil, false
	}
	offset := int(offsetValue.ToInteger())
	length := int(lengthValue.ToInteger())
	if offset < 0 || length < 0 {
		return nil, false
	}
	bytes := buffer.Bytes()
	if offset > len(bytes) || length > len(bytes)-offset {
		return nil, false
	}
	return append([]byte(nil), bytes[offset:offset+length]...), true
}
