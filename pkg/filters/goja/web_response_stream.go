package goja

import (
	"errors"
	"net/http"
	"sync"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

type responseStreamOp struct {
	kind   string
	data   []byte
	result chan error
}

type responseStreamState struct {
	ctx     core.FilterContext
	runtime *runtimeState
	loop    *eventloop.EventLoop

	mu      sync.Mutex
	headers http.Header
	status  int
	started bool
	closed  bool
	pending [][]byte

	done     chan struct{}
	doneOnce sync.Once
	queue    chan responseStreamOp
}

func installResponseStream(ctx core.FilterContext, vm *goja.Runtime, loop *eventloop.EventLoop, runtime *runtimeState, closers *Closers) error {
	return vm.Set("createStreamResponse", func(init ...goja.Value) *goja.Object {
		state := &responseStreamState{
			ctx:     ctx,
			runtime: runtime,
			loop:    loop,
			headers: make(http.Header),
			status:  http.StatusOK,
			done:    make(chan struct{}),
			queue:   make(chan responseStreamOp),
		}
		if len(init) > 0 {
			tmp := &webResponseState{
				status:  state.status,
				headers: cloneHeaderValues(state.headers),
			}
			applyResponseInit(vm, tmp, init[0])
			state.status = tmp.status
			state.headers = tmp.headers
		}
		closers.AddCloser(state.close)
		result := vm.NewObject()
		_ = result.Set("stream", newResponseStreamObject(vm, state))
		_ = result.Set("response", newResponseObject(vm, loop, runtime, &webResponseState{
			status:  state.status,
			headers: cloneHeaderValues(state.headers),
			stream:  state,
		}))
		return result
	})
}

func newResponseStreamObject(vm *goja.Runtime, state *responseStreamState) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("write", func(chunk goja.Value) *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		data, err := bodyBytesFromValue(vm, chunk)
		if err != nil {
			_ = reject(vm.ToValue(err))
			return promise
		}
		if !state.runtime.startTask() {
			_ = reject(vm.ToValue(errRuntimeClosing))
			return promise
		}
		go func() {
			defer state.runtime.finishTask()
			err := state.write(data)
			state.runtime.runOnLoop(state.loop, func(vm *goja.Runtime) {
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
		if !state.runtime.startTask() {
			_ = reject(vm.ToValue(errRuntimeClosing))
			return promise
		}
		go func() {
			defer state.runtime.finishTask()
			err := state.flush()
			state.runtime.runOnLoop(state.loop, func(vm *goja.Runtime) {
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
		state.mu.Lock()
		defer state.mu.Unlock()
		return state.closed
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	return obj
}

func (s *responseStreamState) write(data []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("stream is closed")
	}
	if !s.started {
		s.pending = append(s.pending, append([]byte(nil), data...))
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	result := make(chan error, 1)
	return s.send(responseStreamOp{kind: "write", data: append([]byte(nil), data...), result: result})
}

func (s *responseStreamState) flush() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("stream is closed")
	}
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	result := make(chan error, 1)
	return s.send(responseStreamOp{kind: "flush", result: result})
}

func (s *responseStreamState) close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	result := make(chan error, 1)
	return s.send(responseStreamOp{kind: "close", result: result})
}

func (s *responseStreamState) send(op responseStreamOp) error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-s.done:
		return errors.New("stream is closed")
	case s.queue <- op:
	}
	select {
	case err := <-op.result:
		return err
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-s.done:
		return errors.New("stream is closed")
	}
}

func (s *responseStreamState) serve(writer http.ResponseWriter) error {
	base := utils.BaseWriterOf(writer)
	flusher, ok := base.(http.Flusher)
	if !ok {
		return errors.New("response writer does not support streaming")
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("response stream already started")
	}
	for key, values := range s.headers {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	writer.WriteHeader(s.status)
	s.started = true
	pending := s.pending
	s.pending = nil
	closed := s.closed
	s.mu.Unlock()

	defer s.finish()
	if err := safeHTTPFlush(flusher); err != nil {
		return err
	}
	for _, chunk := range pending {
		if len(chunk) == 0 {
			continue
		}
		if _, err := writer.Write(chunk); err != nil {
			return err
		}
		if err := safeHTTPFlush(flusher); err != nil {
			return err
		}
	}
	if closed {
		return nil
	}

	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case op := <-s.queue:
			switch op.kind {
			case "write":
				_, err := writer.Write(op.data)
				if err == nil {
					err = safeHTTPFlush(flusher)
				}
				op.result <- err
				close(op.result)
				if err != nil {
					return err
				}
			case "flush":
				op.result <- safeHTTPFlush(flusher)
				close(op.result)
			case "close":
				op.result <- nil
				close(op.result)
				return nil
			}
		}
	}
}

func (s *responseStreamState) finish() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
}
