package pkg

import (
	"net/http"
	"time"

	"code.d7z.net/d7z-project/gitea-pages/pkg/core"
	"code.d7z.net/d7z-project/gitea-pages/pkg/utils"
	"github.com/pbnjay/memory"
)

type ServerOptions struct {
	Domain string
	Config utils.Config
	Cache  utils.Cache

	MaxCacheSize int

	HttpClient *http.Client
}

func DefaultOptions(domain string) ServerOptions {
	configMemory, _ := utils.NewConfigMemory("")
	return ServerOptions{
		Domain:       domain,
		Config:       configMemory,
		Cache:        utils.NewCacheMemory(1024*1024*10, int(memory.FreeMemory()/3*2)),
		MaxCacheSize: 1024 * 1024 * 10,
		HttpClient:   http.DefaultClient,
	}
}

type Server struct {
	meta    *core.ServerMeta
	options *ServerOptions
}

func NewPageServer(backend core.Backend, options ServerOptions) *Server {
	backend = core.NewCacheBackend(backend, options.Config, time.Minute)
	return &Server{
		meta:    core.NewServerMeta(options.HttpClient, backend, options.Config, time.Minute),
		options: &options,
	}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {

}

func (s *Server) Close() error {
	if err := s.options.Config.Close(); err != nil {
		return err
	}
	if err := s.options.Cache.Close(); err != nil {
		return err
	}
	return nil
}
