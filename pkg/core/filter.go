package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"go.uber.org/zap"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/subscribe"
	"gopkg.d7z.net/middleware/tools"
)

type FilterContext struct {
	context.Context
	*PageContent
	*PageVFS
	Cache  *tools.TTLCache
	OrgDB  kv.CursorPagedKV
	RepoDB kv.CursorPagedKV
	Event  subscribe.Subscriber

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
		zap.L().Debug(fmt.Sprintf("call filter(%s) before", stack.Type), zap.Any("filter", stack))
		err := call(ctx, writer, request, parentCall)
		zap.L().Debug(fmt.Sprintf("call filter(%s) after", stack.Type), zap.Any("filter", stack), zap.Error(err))
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

type (
	GlobalFilter   func(config Params) (FilterInstance, error)
	FilterInstance func(route Params) (FilterCall, error)
)
