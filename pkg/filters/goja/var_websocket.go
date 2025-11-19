package goja

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/dop251/goja"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

func WebsocketInject(ctx core.FilterContext, jsCtx *goja.Runtime, w http.ResponseWriter, request *http.Request, cancelFunc context.CancelFunc) (io.Closer, error) {
	closers := NewClosers()
	return closers, jsCtx.GlobalObject().Set("websocket", func() (any, error) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, request, nil)
		if err != nil {
			return nil, err
		}
		cancelFunc()
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
		zap.L().Debug("websocket upgrader created")
		closers.AddCloser(conn.Close)
		return map[string]interface{}{
			"on": func(f func(mType int, message string)) {
				go func() {
				z:
					for {
						select {
						case <-ctx.Done():
							break z
						default:
							messageType, p, err := conn.ReadMessage()
							if err != nil {
								break z
							}
							f(messageType, string(p))
						}

					}

				}()
			},
			"TypeTextMessage":   websocket.TextMessage,
			"TypeBinaryMessage": websocket.BinaryMessage,
			"readText": func() (string, error) {
				_, p, err := conn.ReadMessage()
				if err != nil {
					return "", err
				}
				return string(p), nil
			},
			"read": func() (any, error) {
				messageType, p, err := conn.ReadMessage()
				if err != nil {
					return nil, err
				}
				return map[string]interface{}{
					"type": messageType,
					"data": p,
				}, nil
			},
			"writeText": func(data string) error {
				return conn.WriteMessage(websocket.TextMessage, []byte(data))
			},
			"write": func(mType int, data any) error {
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
					return errors.Errorf("invalid type for websocket text: %T", data)
				}
				return conn.WriteMessage(mType, dataRaw)
			},
			"ping": func() error {
				return conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(1*time.Second))
			},
		}, nil
	})
}
