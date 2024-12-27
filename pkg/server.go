package pkg

import (
	"code.d7z.net/d7z-project/gitea-pages/pkg/services"
)

type ServerOptions struct {
	Domain string
	Cache  services.Config
}

func DefaultOptions(domain string) *ServerOptions {
	return &ServerOptions{
		Domain: domain,
		Cache:  services.NewConfigMemory(),
	}
}

type Server struct {
}

func NewServer(backend services.Backend, options *ServerOptions) *Server {

}
