package core

import (
	"context"
	"io"
	"net/http"
	"sync"
)

var (
	renders = make(map[string]Render)
	lock    = &sync.Mutex{}
)

type Render interface {
	Render(ctx context.Context, w http.ResponseWriter, r *http.Request, input io.Reader, meta *PageDomainContent) error
}

func RegisterRender(fType string, r Render) {
	lock.Lock()
	defer lock.Unlock()
	if renders[fType] != nil {
		panic("duplicate render type: " + fType)
	}
	renders[fType] = r
}

func GetRender(key string) Render {
	return renders[key]
}
