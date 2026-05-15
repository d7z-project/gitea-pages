package goja

import (
	"errors"
	"io"
	"sync"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

const (
	defaultStreamChunkSize = 32 << 10
	maxStreamChunkSize     = 1 << 20
)

type readableStreamState struct {
	mu      sync.Mutex
	open    func() (io.ReadCloser, error)
	preOpen func() error
	reader  io.ReadCloser
	opened  bool
	closed  bool
	eof     bool
	reading bool
}

type writableStreamState struct {
	mu      sync.Mutex
	open    func() (io.WriteCloser, error)
	preOpen func() error
	writer  io.WriteCloser
	opened  bool
	closed  bool
	writing bool
}

func newReadableStreamObject(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, state *readableStreamState) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("read", func(options ...goja.Value) *goja.Promise {
		size := defaultStreamChunkSize
		if len(options) > 0 {
			size = streamReadSize(vm, options[0])
		}
		promise, resolve, reject := vm.NewPromise()
		if !runtime.startTask() {
			_ = reject(vm.ToValue(errRuntimeClosing))
			return promise
		}
		go func() {
			defer runtime.finishTask()
			value, done, err := state.read(size)
			runtime.runOnLoop(loop, func(vm *goja.Runtime) {
				if err != nil {
					_ = reject(vm.ToValue(err))
					return
				}
				result := map[string]any{"done": done}
				if !done {
					result["value"] = uint8ArrayValue(vm, value)
				}
				_ = resolve(vm.ToValue(result))
			})
		}()
		return promise
	})
	_ = obj.Set("cancel", func(_ ...goja.Value) *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		if err := state.close(); err != nil {
			_ = reject(vm.ToValue(err))
			return promise
		}
		_ = resolve(goja.Undefined())
		return promise
	})
	_ = obj.Set("close", func() *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		if err := state.close(); err != nil {
			_ = reject(vm.ToValue(err))
			return promise
		}
		_ = resolve(goja.Undefined())
		return promise
	})
	_ = obj.DefineAccessorProperty("closed", vm.ToValue(func() bool {
		return state.isClosed()
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	return obj
}

func newWritableStreamObject(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, state *writableStreamState, flushFn func() error) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("write", func(chunk goja.Value) *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		data, err := bodyBytesFromValue(vm, chunk)
		if err != nil {
			_ = reject(vm.ToValue(err))
			return promise
		}
		if !runtime.startTask() {
			_ = reject(vm.ToValue(errRuntimeClosing))
			return promise
		}
		go func() {
			defer runtime.finishTask()
			err := state.write(data)
			runtime.runOnLoop(loop, func(vm *goja.Runtime) {
				if err != nil {
					_ = reject(vm.ToValue(err))
					return
				}
				_ = resolve(goja.Undefined())
			})
		}()
		return promise
	})
	_ = obj.Set("flush", func() *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		if flushFn == nil {
			_ = resolve(goja.Undefined())
			return promise
		}
		if !runtime.startTask() {
			_ = reject(vm.ToValue(errRuntimeClosing))
			return promise
		}
		go func() {
			defer runtime.finishTask()
			err := flushFn()
			runtime.runOnLoop(loop, func(vm *goja.Runtime) {
				if err != nil {
					_ = reject(vm.ToValue(err))
					return
				}
				_ = resolve(goja.Undefined())
			})
		}()
		return promise
	})
	_ = obj.Set("abort", func(_ ...goja.Value) *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		if err := state.close(); err != nil {
			_ = reject(vm.ToValue(err))
			return promise
		}
		_ = resolve(goja.Undefined())
		return promise
	})
	_ = obj.Set("close", func() *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		if err := state.close(); err != nil {
			_ = reject(vm.ToValue(err))
			return promise
		}
		_ = resolve(goja.Undefined())
		return promise
	})
	_ = obj.DefineAccessorProperty("closed", vm.ToValue(func() bool {
		return state.isClosed()
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	return obj
}

func streamReadSize(vm *goja.Runtime, value goja.Value) int {
	if isNilish(value) {
		return defaultStreamChunkSize
	}
	obj, ok := valueObject(vm, value)
	if !ok {
		return defaultStreamChunkSize
	}
	size, ok := objectInt64(obj, "size")
	if !ok || size <= 0 {
		return defaultStreamChunkSize
	}
	if size > maxStreamChunkSize {
		return maxStreamChunkSize
	}
	return int(size)
}

func (s *readableStreamState) read(size int) ([]byte, bool, error) {
	s.mu.Lock()
	if s.eof {
		s.mu.Unlock()
		return nil, true, nil
	}
	if s.closed {
		s.mu.Unlock()
		return nil, false, errors.New("stream is closed")
	}
	if s.reading {
		s.mu.Unlock()
		return nil, false, errors.New("concurrent read is not allowed")
	}
	if !s.opened {
		if s.preOpen != nil {
			if err := s.preOpen(); err != nil {
				s.mu.Unlock()
				return nil, false, err
			}
		}
		reader, err := s.open()
		if err != nil {
			s.closed = true
			s.mu.Unlock()
			return nil, false, err
		}
		s.reader = reader
		s.opened = true
	}
	reader := s.reader
	s.reading = true
	s.mu.Unlock()

	buf := make([]byte, size)
	n, err := reader.Read(buf)

	s.mu.Lock()
	s.reading = false
	if errors.Is(err, io.EOF) {
		if n == 0 {
			s.eof = true
			closeErr := s.closeReaderLocked()
			s.mu.Unlock()
			return nil, true, closeErr
		}
		s.eof = true
		closeErr := s.closeReaderLocked()
		s.mu.Unlock()
		if closeErr != nil {
			return nil, false, closeErr
		}
		return buf[:n], false, nil
	}
	if err != nil {
		s.closed = true
		_ = s.closeReaderLocked()
		s.mu.Unlock()
		return nil, false, err
	}
	s.mu.Unlock()
	return buf[:n], false, nil
}

func (s *readableStreamState) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.closeReaderLocked()
}

func (s *readableStreamState) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed || s.eof
}

func (s *readableStreamState) closeReaderLocked() error {
	if s.reader == nil {
		return nil
	}
	reader := s.reader
	s.reader = nil
	return reader.Close()
}

func (s *writableStreamState) write(data []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("stream is closed")
	}
	if s.writing {
		s.mu.Unlock()
		return errors.New("concurrent write is not allowed")
	}
	if !s.opened {
		if s.preOpen != nil {
			if err := s.preOpen(); err != nil {
				s.mu.Unlock()
				return err
			}
		}
		writer, err := s.open()
		if err != nil {
			s.closed = true
			s.mu.Unlock()
			return err
		}
		s.writer = writer
		s.opened = true
	}
	writer := s.writer
	s.writing = true
	s.mu.Unlock()

	_, err := writer.Write(data)

	s.mu.Lock()
	s.writing = false
	if err != nil {
		s.closed = true
		_ = s.closeWriterLocked()
	}
	s.mu.Unlock()
	return err
}

func (s *writableStreamState) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.closeWriterLocked()
}

func (s *writableStreamState) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *writableStreamState) closeWriterLocked() error {
	if s.writer == nil {
		return nil
	}
	writer := s.writer
	s.writer = nil
	return writer.Close()
}
