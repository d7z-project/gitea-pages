package goja

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/gitea-pages/pkg/utils"
)

type sseMessage struct {
	payload string
	result  chan error
}

type sseState struct {
	ctx       core.FilterContext
	runtime   *runtimeState
	loop      *eventloop.EventLoop
	ready     chan struct{}
	done      chan struct{}
	queue     chan sseMessage
	closeCh   chan struct{}
	closeOnce sync.Once
}

func installSSE(ctx core.FilterContext, vm *goja.Runtime, writer http.ResponseWriter, loop *eventloop.EventLoop, runtime *runtimeState) (io.Closer, error) {
	closers := NewClosers()
	if err := vm.Set("createEventStream", func(_ ...goja.Value) *goja.Object {
		state := &sseState{
			ctx:     ctx,
			runtime: runtime,
			loop:    loop,
			ready:   make(chan struct{}),
			done:    make(chan struct{}),
			queue:   make(chan sseMessage),
			closeCh: make(chan struct{}),
		}
		responseObj := newResponseObject(vm, &webResponseState{
			status: http.StatusOK,
			headers: http.Header{
				"Content-Type":      []string{"text/event-stream; charset=utf-8"},
				"Cache-Control":     []string{"no-cache"},
				"Connection":        []string{"keep-alive"},
				"X-Accel-Buffering": []string{"no"},
			},
			upgrade: func() error {
				base := utils.BaseWriterOf(writer)
				flusher, ok := base.(http.Flusher)
				if !ok {
					return errors.New("response writer does not support streaming")
				}
				writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				writer.Header().Set("Cache-Control", "no-cache")
				writer.Header().Set("Connection", "keep-alive")
				writer.Header().Set("X-Accel-Buffering", "no")
				writer.WriteHeader(http.StatusOK)
				if err := safeSSEFlush(flusher); err != nil {
					state.finish()
					return err
				}
				close(state.ready)
				for {
					select {
					case <-ctx.Done():
						state.finish()
						return ctx.Err()
					case <-state.closeCh:
						state.finish()
						return nil
					case msg := <-state.queue:
						_, err := io.WriteString(writer, msg.payload)
						if err == nil {
							err = safeSSEFlush(flusher)
						}
						msg.result <- err
						close(msg.result)
						if err != nil {
							state.finish()
							return err
						}
					}
				}
			},
		})
		streamObj := newSSEStreamObject(vm, state)
		result := vm.NewObject()
		_ = result.Set("stream", streamObj)
		_ = result.Set("response", responseObj)
		return result
	}); err != nil {
		return nil, err
	}
	return closers, nil
}

func newSSEStreamObject(vm *goja.Runtime, state *sseState) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("send", func(data string, options ...goja.Value) *goja.Promise {
		promise, resolve, reject := vm.NewPromise()
		settle := func(err error) {
			state.runtime.runOnLoop(state.loop, func(vm *goja.Runtime) {
				if err != nil {
					_ = reject(vm.ToValue(err.Error()))
					return
				}
				_ = resolve(goja.Undefined())
			})
		}
		if !state.runtime.startTask() {
			_ = reject(vm.ToValue(errRuntimeClosing.Error()))
			return promise
		}
		go func() {
			defer state.runtime.finishTask()
			select {
			case <-state.ready:
			case <-state.ctx.Done():
				settle(state.ctx.Err())
				return
			case <-state.done:
				settle(errors.New("event stream is closed"))
				return
			}
			result := make(chan error, 1)
			msg := sseMessage{
				payload: encodeSSEPayload(vm, data, options),
				result:  result,
			}
			select {
			case <-state.ctx.Done():
				settle(state.ctx.Err())
				return
			case <-state.done:
				settle(errors.New("event stream is closed"))
				return
			case state.queue <- msg:
			}
			select {
			case err := <-result:
				settle(err)
			case <-state.ctx.Done():
				settle(state.ctx.Err())
			case <-state.done:
				settle(errors.New("event stream is closed"))
			}
		}()
		return promise
	})
	_ = obj.Set("close", func() {
		state.finish()
	})
	return obj
}

func safeSSEFlush(flusher http.Flusher) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("sse flush panic: %v", r)
		}
	}()
	flusher.Flush()
	return nil
}

func encodeSSEPayload(vm *goja.Runtime, data string, options []goja.Value) string {
	eventName := ""
	eventID := ""
	retry := int64(0)
	if len(options) > 0 && !isNilish(options[0]) {
		if obj, ok := valueObject(vm, options[0]); ok {
			if value, ok := objectString(obj, "event"); ok {
				eventName = value
			}
			if value, ok := objectString(obj, "id"); ok {
				eventID = value
			}
			if value, ok := objectInt64(obj, "retry"); ok {
				retry = value
			}
		}
	}
	var payload strings.Builder
	if eventName != "" {
		payload.WriteString("event: ")
		payload.WriteString(eventName)
		payload.WriteByte('\n')
	}
	if eventID != "" {
		payload.WriteString("id: ")
		payload.WriteString(eventID)
		payload.WriteByte('\n')
	}
	if retry > 0 {
		_, _ = fmt.Fprintf(&payload, "retry: %d\n", retry)
	}
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r", ""), "\n") {
		payload.WriteString("data: ")
		payload.WriteString(line)
		payload.WriteByte('\n')
	}
	payload.WriteByte('\n')
	return payload.String()
}

func (s *sseState) finish() {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		close(s.done)
	})
}
