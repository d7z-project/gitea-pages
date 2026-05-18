package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/storage"
	"gopkg.d7z.net/middleware/subscribe"
	"gopkg.d7z.net/middleware/tools"
)

type FilterContext struct {
	context.Context
	*PageContent
	*PageVFS
	Cache        *tools.TTLCache
	OrgDB        kv.KV
	RepoDB       kv.KV
	Storage      storage.Storage
	VersionEvent subscribe.Subscriber
	SharedEvent  subscribe.Subscriber
	Auth         AuthInfo

	Kill func()
}

type Params map[string]any

func (f Params) String() string {
	marshal, _ := json.Marshal(f)
	return strings.ReplaceAll(string(marshal), "\"", "'")
}

func (f Params) Unmarshal(target any) error {
	marshal, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return json.Unmarshal(marshal, target)
}

type Filter struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Params Params `json:"params"`
}

func NextCallWrapper(call FilterCall, parentCall NextCall, stack Filter) NextCall {
	return func(ctx FilterContext, writer http.ResponseWriter, request *http.Request) error {
		slog.Debug(fmt.Sprintf("call filter(%s) before", stack.Type), "filter", stack)
		err := call(ctx, writer, request, parentCall)
		slog.Debug(fmt.Sprintf("call filter(%s) after", stack.Type), "filter", stack, "error", err)
		return err
	}
}

type NextCall func(
	ctx FilterContext,
	writer http.ResponseWriter,
	request *http.Request,
) error

var NotFountNextCall = func(ctx FilterContext, writer http.ResponseWriter, request *http.Request) error {
	return os.ErrNotExist
}

type FilterCall func(
	ctx FilterContext,
	writer http.ResponseWriter,
	request *http.Request,
	next NextCall,
) error

type FilterServerConfig struct {
	StaticCacheControl  string
	MaxRequestBodyBytes int64
}

type GlobalFilterInit struct {
	Config Params
	Server FilterServerConfig
}

type (
	GlobalFilter   func(init GlobalFilterInit) (FilterInstance, error)
	FilterInstance func(route Params) (FilterCall, error)
)
