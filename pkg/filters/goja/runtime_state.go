package goja

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

var errRuntimeClosing = errors.New("runtime is closing")

type runtimeState struct {
	closing atomic.Bool
	tasks   sync.WaitGroup
}

func (r *runtimeState) startTask() bool {
	if r == nil || r.closing.Load() {
		return false
	}
	r.tasks.Add(1)
	if r.closing.Load() {
		r.tasks.Done()
		return false
	}
	return true
}

func (r *runtimeState) finishTask() {
	if r == nil {
		return
	}
	r.tasks.Done()
}

func (r *runtimeState) beginClosing() {
	if r == nil {
		return
	}
	r.closing.Store(true)
}

func (r *runtimeState) isClosing() bool {
	return r != nil && r.closing.Load()
}

func (r *runtimeState) runOnLoop(loop *eventloop.EventLoop, fn func(*goja.Runtime)) bool {
	if r != nil && r.closing.Load() {
		return false
	}
	loop.RunOnLoop(func(vm *goja.Runtime) {
		if r != nil && r.closing.Load() {
			return
		}
		fn(vm)
	})
	return true
}

func (r *runtimeState) wait(timeout time.Duration) bool {
	if r == nil {
		return true
	}
	done := make(chan struct{})
	go func() {
		r.tasks.Wait()
		close(done)
	}()
	if timeout <= 0 {
		<-done
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
