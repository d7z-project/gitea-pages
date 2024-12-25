package pkg

import (
	"code.d7z.net/d7z-project/gitea-pages/pkg/services"
)

type ServerOptions struct {
	Domain string
}

type Server struct {
}

func NewServer(backend services.Backend, options *ServerOptions) *Server {

}
