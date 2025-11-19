package goja

import (
	"context"
	"io"
	"net/http"

	"github.com/dop251/goja"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func WebsocketInject(jsCtx *goja.Runtime, w http.ResponseWriter, request *http.Request, cancelFunc context.CancelFunc) (io.Closer, error) {
	closers := NewClosers()
	return closers, jsCtx.GlobalObject().Set("websocket", func() (any, error) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, request, nil)
		if err != nil {
			return nil, err
		}
		cancelFunc()
		zap.L().Debug("websocket upgrader created")
		closers.AddCloser(conn.Close)
		return map[string]interface{}{
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
		}, nil
	})
}
