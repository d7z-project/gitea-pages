package goja

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/gorilla/websocket"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

type webSocketState struct {
	loop      *eventloop.EventLoop
	vm        *goja.Runtime
	runtime   *runtimeState
	ctx       core.FilterContext
	written   func() bool
	conn      *websocket.Conn
	connMu    sync.RWMutex
	writeMu   sync.Mutex
	done      chan struct{}
	doneOnce  sync.Once
	activated atomic.Bool
	listeners map[string][]goja.Callable
	onopen    goja.Callable
	onmessage goja.Callable
	onerror   goja.Callable
	onclose   goja.Callable
}

func installWebSocket(ctx core.FilterContext, vm *goja.Runtime, writer http.ResponseWriter, request *http.Request, loop *eventloop.EventLoop, runtime *runtimeState) (io.Closer, error) {
	closers := NewClosers()
	if err := vm.Set("upgradeWebSocket", func(_ ...goja.Value) (*goja.Object, error) {
		socketState := &webSocketState{
			loop:      loop,
			vm:        vm,
			runtime:   runtime,
			ctx:       ctx,
			written:   writtenResponseFunc(writer),
			done:      make(chan struct{}),
			listeners: make(map[string][]goja.Callable),
		}
		closers.AddCloser(func() error {
			socketState.closeConn(websocket.CloseGoingAway)
			socketState.finish()
			return nil
		})
		socketObj := newWebSocketObject(vm, socketState)
		responseObj := newResponseObject(vm, loop, runtime, &webResponseState{
			status: http.StatusSwitchingProtocols,
			headers: http.Header{
				"Connection": []string{"Upgrade"},
				"Upgrade":    []string{"websocket"},
			},
			upgrade: func() error { return socketState.serve(writer, request, closers) },
		})
		result := vm.NewObject()
		_ = result.Set("socket", socketObj)
		_ = result.Set("response", responseObj)
		return result, nil
	}); err != nil {
		return nil, err
	}
	return closers, nil
}

func newWebSocketObject(vm *goja.Runtime, state *webSocketState) *goja.Object {
	obj := vm.NewObject()
	_ = obj.Set("CONNECTING", 0)
	_ = obj.Set("OPEN", 1)
	_ = obj.Set("CLOSING", 2)
	_ = obj.Set("CLOSED", 3)
	_ = obj.DefineAccessorProperty("readyState", vm.ToValue(func() int {
		state.connMu.RLock()
		defer state.connMu.RUnlock()
		if state.conn == nil {
			select {
			case <-state.done:
				return 3
			default:
				return 0
			}
		}
		select {
		case <-state.done:
			return 3
		default:
			return 1
		}
	}), goja.Undefined(), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.Set("addEventListener", func(name string, fn goja.Callable) {
		state.listeners[name] = append(state.listeners[name], fn)
	})
	_ = obj.Set("send", func(data goja.Value) *goja.Promise {
		if state.unavailable() {
			return rejectedPromise(vm, responseIOUnavailableError("websocket"))
		}
		messageType := websocket.TextMessage
		payload, err := bodyBytesFromValue(vm, data)
		if err != nil {
			return rejectedPromise(vm, err)
		}
		if isNilish(data) {
			messageType = websocket.BinaryMessage
		} else if _, ok := data.Export().(string); !ok {
			messageType = websocket.BinaryMessage
		}
		return asyncVoidPromise(vm, state.loop, state.runtime, func() error {
			conn, ok := state.currentConn()
			if !ok {
				return errors.New("websocket is not open")
			}
			state.writeMu.Lock()
			err := conn.WriteMessage(messageType, payload)
			state.writeMu.Unlock()
			return err
		})
	})
	_ = obj.Set("close", func(code ...int) {
		closeCode := websocket.CloseNormalClosure
		if len(code) > 0 {
			closeCode = code[0]
		}
		state.closeConn(closeCode)
		state.finish()
	})
	_ = obj.DefineAccessorProperty("onopen", vm.ToValue(func() goja.Value {
		if state.onopen == nil {
			return goja.Null()
		}
		return vm.ToValue(state.onopen)
	}), vm.ToValue(func(value goja.Value) {
		state.onopen = callableFromValue(value)
	}), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.DefineAccessorProperty("onmessage", vm.ToValue(func() goja.Value {
		if state.onmessage == nil {
			return goja.Null()
		}
		return vm.ToValue(state.onmessage)
	}), vm.ToValue(func(value goja.Value) {
		state.onmessage = callableFromValue(value)
	}), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.DefineAccessorProperty("onerror", vm.ToValue(func() goja.Value {
		if state.onerror == nil {
			return goja.Null()
		}
		return vm.ToValue(state.onerror)
	}), vm.ToValue(func(value goja.Value) {
		state.onerror = callableFromValue(value)
	}), goja.FLAG_FALSE, goja.FLAG_TRUE)
	_ = obj.DefineAccessorProperty("onclose", vm.ToValue(func() goja.Value {
		if state.onclose == nil {
			return goja.Null()
		}
		return vm.ToValue(state.onclose)
	}), vm.ToValue(func(value goja.Value) {
		state.onclose = callableFromValue(value)
	}), goja.FLAG_FALSE, goja.FLAG_TRUE)
	return obj
}

func (s *webSocketState) start() {
	s.dispatch("open", nil)
	if s.runtime.startTask() {
		go s.pingLoop()
	}
	if s.runtime.startTask() {
		go s.readLoop()
	}
}

func (s *webSocketState) unavailable() bool {
	return s.written() && !s.activated.Load()
}

func (s *webSocketState) serve(writer http.ResponseWriter, request *http.Request, closers *Closers) error {
	if s.unavailable() {
		return responseIOUnavailableError("websocket")
	}
	upgrader := websocket.Upgrader{}
	s.activated.Store(true)
	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		s.activated.Store(false)
		return err
	}
	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
	closers.AddCloser(conn.Close)
	s.start()
	<-s.done
	return nil
}

func (s *webSocketState) pingLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer s.runtime.finishTask()
	for {
		select {
		case <-s.ctx.Done():
			s.closeConn(websocket.CloseGoingAway)
			s.finish()
			return
		case <-s.done:
			return
		case <-ticker.C:
			conn, ok := s.currentConn()
			if !ok {
				return
			}
			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
				slog.Debug("websocket ping failed", "error", err)
				s.dispatch("error", map[string]any{"error": err.Error()})
				s.ctx.Kill()
				s.closeConn(websocket.CloseAbnormalClosure)
				s.finish()
				return
			}
		}
	}
}

func (s *webSocketState) readLoop() {
	defer s.runtime.finishTask()
	defer s.finish()
	for {
		conn, ok := s.currentConn()
		if !ok {
			return
		}
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			s.dispatch("close", nil)
			return
		}
		switch messageType {
		case websocket.TextMessage:
			s.dispatch("message", map[string]any{"data": string(payload)})
		case websocket.BinaryMessage:
			s.dispatch("message", map[string]any{"data": append([]byte(nil), payload...)})
		}
	}
}

func (s *webSocketState) currentConn() (*websocket.Conn, bool) {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.conn, s.conn != nil
}

func (s *webSocketState) finish() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
}

func (s *webSocketState) closeConn(code int) {
	conn, ok := s.currentConn()
	if !ok {
		return
	}
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, ""), time.Now().Add(5*time.Second))
	_ = conn.Close()
}

func (s *webSocketState) dispatch(name string, payload map[string]any) {
	s.runtime.runOnLoop(s.loop, func(vm *goja.Runtime) {
		event := vm.NewObject()
		for key, value := range payload {
			_ = event.Set(key, value)
		}
		for _, fn := range s.listeners[name] {
			_, _ = fn(goja.Undefined(), event)
		}
		switch name {
		case "open":
			if s.onopen != nil {
				_, _ = s.onopen(goja.Undefined(), event)
			}
		case "message":
			if s.onmessage != nil {
				_, _ = s.onmessage(goja.Undefined(), event)
			}
		case "error":
			if s.onerror != nil {
				_, _ = s.onerror(goja.Undefined(), event)
			}
		case "close":
			if s.onclose != nil {
				_, _ = s.onclose(goja.Undefined(), event)
			}
		}
	})
}

func callableFromValue(value goja.Value) goja.Callable {
	if isNilish(value) {
		return nil
	}
	fn, ok := goja.AssertFunction(value)
	if !ok {
		return nil
	}
	return fn
}
