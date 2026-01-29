package goja

import (
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func WebsocketInject(ctx core.FilterContext, jsCtx *goja.Runtime, w http.ResponseWriter, request *http.Request, loop *eventloop.EventLoop) (io.Closer, error) {
	closers := NewClosers()
	return closers, jsCtx.GlobalObject().Set("websocket", func() (any, error) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, request, nil)
		if err != nil {
			return nil, err
		}
		var readMu sync.Mutex
		var writeMu sync.Mutex

		zap.L().Debug("websocket upgrader created")
		closers.AddCloser(conn.Close)
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
		f:
			for {
				select {
				case <-ctx.Done():
					break f
				case <-ticker.C:
				}
				if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second)); err != nil {
					zap.L().Debug("websocket ping failed", zap.Error(err))
					ctx.Kill()
				}
			}
		}()
		return map[string]interface{}{
			"TypeTextMessage":   websocket.TextMessage,
			"TypeBinaryMessage": websocket.BinaryMessage,
			"readText": func() *goja.Promise {
				promise, resolve, reject := jsCtx.NewPromise()
				go func() {
					select {
					case <-ctx.Done():
						loop.RunOnLoop(func(runtime *goja.Runtime) {
							_ = reject(runtime.ToValue(ctx.Err()))
						})
						return
					default:
					}
					defer func() {
						if r := recover(); r != nil {
							zap.L().Debug("websocket panic", zap.Any("panic", r))
							loop.RunOnLoop(func(runtime *goja.Runtime) {
								_ = reject(runtime.ToValue(r))
							})
						}
					}()
					readMu.Lock()
					_, p, err := conn.ReadMessage()
					readMu.Unlock()
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						if err != nil {
							_ = reject(runtime.ToValue(err))
						} else {
							_ = resolve(runtime.ToValue(string(p)))
						}
					})
				}()
				return promise
			},
			"read": func() *goja.Promise {
				promise, resolve, reject := jsCtx.NewPromise()
				go func() {
					select {
					case <-ctx.Done():
						loop.RunOnLoop(func(runtime *goja.Runtime) {
							_ = reject(runtime.ToValue(ctx.Err()))
						})
						return
					default:
					}
					defer func() {
						if r := recover(); r != nil {
							zap.L().Debug("websocket panic", zap.Any("panic", r))
							loop.RunOnLoop(func(runtime *goja.Runtime) {
								_ = reject(runtime.ToValue(r))
							})
						}
					}()
					readMu.Lock()
					messageType, p, err := conn.ReadMessage()
					readMu.Unlock()
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						if err != nil {
							_ = reject(runtime.ToValue(err))
						} else {
							_ = resolve(runtime.ToValue(map[string]interface{}{
								"type": messageType,
								"data": p,
							}))
						}
					})
				}()
				return promise
			},
			"writeText": func(data string) *goja.Promise {
				promise, resolve, reject := jsCtx.NewPromise()
				go func() {
					select {
					case <-ctx.Done():
						loop.RunOnLoop(func(runtime *goja.Runtime) {
							_ = reject(runtime.ToValue(ctx.Err()))
						})
						return
					default:
					}
					defer func() {
						if r := recover(); r != nil {
							zap.L().Debug("websocket panic", zap.Any("panic", r))
							loop.RunOnLoop(func(runtime *goja.Runtime) {
								_ = reject(runtime.ToValue(r))
							})
						}
					}()
					writeMu.Lock()
					err := conn.WriteMessage(websocket.TextMessage, []byte(data))
					writeMu.Unlock()
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						if err != nil {
							_ = reject(runtime.ToValue(err))
						} else {
							_ = resolve(runtime.ToValue(nil))
						}
					})
				}()
				return promise
			},
			"write": func(mType int, data any) *goja.Promise {
				promise, resolve, reject := jsCtx.NewPromise()
				go func() {
					select {
					case <-ctx.Done():
						loop.RunOnLoop(func(runtime *goja.Runtime) {
							_ = reject(runtime.ToValue(ctx.Err()))
						})
						return
					default:
					}
					defer func() {
						if r := recover(); r != nil {
							zap.L().Debug("websocket panic", zap.Any("panic", r))
							loop.RunOnLoop(func(runtime *goja.Runtime) {
								_ = reject(runtime.ToValue(r))
							})
						}
					}()

					if item, ok := data.(goja.Value); ok {
						data = item.Export()
					}
					var dataRaw []byte
					switch it := data.(type) {
					case []byte:
						dataRaw = it
					case string:
						dataRaw = []byte(it)
					default:
						loop.RunOnLoop(func(runtime *goja.Runtime) {
							_ = reject(runtime.ToValue(errors.Errorf("invalid type for websocket text: %T", data)))
						})
						return
					}

					writeMu.Lock()
					err := conn.WriteMessage(mType, dataRaw)
					writeMu.Unlock()
					loop.RunOnLoop(func(runtime *goja.Runtime) {
						if err != nil {
							_ = reject(runtime.ToValue(err))
						} else {
							_ = resolve(goja.Undefined())
						}
					})
				}()
				return promise
			},
		}, nil
	})
}
