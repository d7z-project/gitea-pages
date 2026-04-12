package goja

import (
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

type webSocketState struct {
	loop      *eventloop.EventLoop
	vm        *goja.Runtime
	ctx       core.FilterContext
	conn      *websocket.Conn
	connMu    sync.RWMutex
	writeMu   sync.Mutex
	done      chan struct{}
	doneOnce  sync.Once
	listeners map[string][]goja.Callable
	onopen    goja.Callable
	onmessage goja.Callable
	onerror   goja.Callable
	onclose   goja.Callable
}

func installWebSocket(ctx core.FilterContext, vm *goja.Runtime, writer http.ResponseWriter, request *http.Request, loop *eventloop.EventLoop) (io.Closer, error) {
	closers := NewClosers()
	if err := vm.Set("upgradeWebSocket", func(_ ...goja.Value) (*goja.Object, error) {
		socketState := &webSocketState{
			loop:      loop,
			vm:        vm,
			ctx:       ctx,
			done:      make(chan struct{}),
			listeners: make(map[string][]goja.Callable),
		}
		socketObj := newWebSocketObject(vm, socketState)
		responseObj := newResponseObject(vm, &webResponseState{
			status: http.StatusSwitchingProtocols,
			headers: http.Header{
				"Connection": []string{"Upgrade"},
				"Upgrade":    []string{"websocket"},
			},
			upgrade: func() error {
				upgrader := websocket.Upgrader{}
				conn, err := upgrader.Upgrade(writer, request, nil)
				if err != nil {
					return err
				}
				socketState.connMu.Lock()
				socketState.conn = conn
				socketState.connMu.Unlock()
				closers.AddCloser(conn.Close)
				socketState.start()
				<-socketState.done
				return nil
			},
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
		promise, resolve, reject := vm.NewPromise()
		go func() {
			conn, ok := state.currentConn()
			if !ok {
				state.loop.RunOnLoop(func(runtime *goja.Runtime) {
					_ = reject(runtime.ToValue("websocket is not open"))
				})
				return
			}
			messageType := websocket.TextMessage
			payload, err := bodyBytesFromValue(vm, data)
			if err != nil {
				state.loop.RunOnLoop(func(runtime *goja.Runtime) {
					_ = reject(runtime.ToValue(err))
				})
				return
			}
			if _, ok := data.Export().(string); !ok {
				messageType = websocket.BinaryMessage
			}
			state.writeMu.Lock()
			err = conn.WriteMessage(messageType, payload)
			state.writeMu.Unlock()
			state.loop.RunOnLoop(func(runtime *goja.Runtime) {
				if err != nil {
					_ = reject(runtime.ToValue(err))
					return
				}
				_ = resolve(goja.Undefined())
			})
		}()
		return promise
	})
	_ = obj.Set("close", func(code ...int) {
		conn, ok := state.currentConn()
		if !ok {
			state.finish()
			return
		}
		closeCode := websocket.CloseNormalClosure
		if len(code) > 0 {
			closeCode = code[0]
		}
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, ""), time.Now().Add(5*time.Second))
		_ = conn.Close()
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
	go s.pingLoop()
	go s.readLoop()
}

func (s *webSocketState) pingLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
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
				zap.L().Debug("websocket ping failed", zap.Error(err))
				s.dispatch("error", map[string]any{"error": err.Error()})
				s.ctx.Kill()
				s.finish()
				return
			}
		}
	}
}

func (s *webSocketState) readLoop() {
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

func (s *webSocketState) dispatch(name string, payload map[string]any) {
	s.loop.RunOnLoop(func(runtime *goja.Runtime) {
		event := runtime.NewObject()
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
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil
	}
	fn, ok := goja.AssertFunction(value)
	if !ok {
		return nil
	}
	return fn
}
