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
		type readResult struct {
			value []byte
			done  bool
		}
		return asyncValuePromise(vm, loop, runtime, func() (readResult, error) {
			value, done, err := state.read(size)
			return readResult{value: value, done: done}, err
		}, func(vm *goja.Runtime, result readResult) (goja.Value, error) {
			payload := map[string]any{"done": result.done}
			if !result.done {
				payload["value"] = uint8ArrayValue(vm, result.value)
			}
			return vm.ToValue(payload), nil
		})
	})
	_ = obj.Set("cancel", func(_ ...goja.Value) *goja.Promise {
		return syncVoidPromise(vm, state.close)
	})
	_ = obj.Set("close", func() *goja.Promise {
		return syncVoidPromise(vm, state.close)
	})
	defineClosedAccessor(obj, vm, func() bool {
		return state.isClosed()
	})
	return obj
}

func newWritableStreamObject(vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, state *writableStreamState, flushFn func() error) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("write", func(chunk goja.Value) *goja.Promise {
		data, err := bodyBytesFromValue(vm, chunk)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return asyncVoidPromise(vm, loop, runtime, func() error {
			return state.write(data)
		})
	})
	_ = obj.Set("flush", func() *goja.Promise {
		if flushFn == nil {
			return resolvedPromise(vm, goja.Undefined())
		}
		return asyncVoidPromise(vm, loop, runtime, flushFn)
	})
	_ = obj.Set("abort", func(_ ...goja.Value) *goja.Promise {
		return syncVoidPromise(vm, state.close)
	})
	_ = obj.Set("close", func() *goja.Promise {
		return syncVoidPromise(vm, state.close)
	})
	defineClosedAccessor(obj, vm, func() bool {
		return state.isClosed()
	})
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
