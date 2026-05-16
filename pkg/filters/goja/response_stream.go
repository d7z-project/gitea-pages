package goja

import (
	"errors"
	"net/http"
	"sync"
	"time"

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
	written func() bool

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

func installResponseStream(ctx core.FilterContext, vm *goja.Runtime, writer http.ResponseWriter, loop *eventloop.EventLoop, runtime *runtimeState, closers *Closers) error {
	return vm.Set("createStreamResponse", func(init ...goja.Value) *goja.Object {
		state := &responseStreamState{
			ctx:     ctx,
			runtime: runtime,
			loop:    loop,
			written: writtenResponseFunc(writer),
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
		data, err := bodyBytesFromValue(vm, chunk)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		return asyncVoidPromise(vm, state.loop, state.runtime, func() error {
			return state.write(data)
		})
	})
	_ = obj.Set("flush", func() *goja.Promise {
		return asyncVoidPromise(vm, state.loop, state.runtime, state.flush)
	})
	_ = obj.Set("ready", func() *goja.Promise {
		return asyncVoidPromise(vm, state.loop, state.runtime, state.ready)
	})
	_ = obj.Set("abort", func(_ ...goja.Value) *goja.Promise {
		return syncVoidPromise(vm, state.close)
	})
	_ = obj.Set("close", func() *goja.Promise {
		return syncVoidPromise(vm, state.close)
	})
	defineClosedAccessor(obj, vm, func() bool {
		state.mu.Lock()
		defer state.mu.Unlock()
		return state.closed
	})
	return obj
}

func (s *responseStreamState) write(data []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("stream is closed")
	}
	if !s.started {
		if s.written() {
			s.mu.Unlock()
			return responseIOUnavailableError("response stream")
		}
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
		if s.written() {
			s.mu.Unlock()
			return responseIOUnavailableError("response stream")
		}
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

func (s *responseStreamState) ready() error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		started := s.started
		closed := s.closed
		written := !started && s.written()
		s.mu.Unlock()
		if started {
			return nil
		}
		if written {
			return responseIOUnavailableError("response stream")
		}
		if closed {
			return errors.New("stream is closed")
		}
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-s.done:
			return errors.New("stream is closed")
		case <-ticker.C:
		}
	}
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
	if s.written() {
		s.mu.Unlock()
		return responseIOUnavailableError("response stream")
	}
	for key, values := range s.headers {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	s.started = true
	writer.WriteHeader(s.status)
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
