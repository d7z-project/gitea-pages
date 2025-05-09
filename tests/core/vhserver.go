package core

import (
	"fmt"
	"net"
	"net/http"

	"go.uber.org/zap"
)

type VServer struct {
	URL      string
	mux      *http.ServeMux
	listener net.Listener
}

func NewServer() *VServer {
	listener, err := net.Listen("tcp", ":0") // ":0" 表示让系统自动选择一个可用的端口
	if err != nil {
		panic(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		zap.L().Debug("ServeHTTP", zap.String("url", r.URL.String()))
	})
	go func() {
		_ = http.Serve(listener, mux)
	}()

	return &VServer{
		listener: listener,
		URL:      fmt.Sprintf("http://127.0.0.1:%d", port),
		mux:      mux,
	}
}

func (v *VServer) Add(path, data string) {
	v.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(data))
	})
}

func (v *VServer) Close() error {
	return v.listener.Close()
}
