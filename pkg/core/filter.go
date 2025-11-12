package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"go.uber.org/zap"
)

type FilterParams map[string]any

func (f FilterParams) String() string {
	marshal, _ := json.Marshal(f)
	return strings.ReplaceAll(string(marshal), "\"", "'")
}

func (f FilterParams) Unmarshal(target any) error {
	marshal, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return json.Unmarshal(marshal, target)
}

type Filter struct {
	Path   string       `json:"path"`
	Type   string       `json:"type"`
	Params FilterParams `json:"params"`
}

func NextCallWrapper(call FilterCall, parentCall NextCall, stack Filter) NextCall {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, metadata *PageDomainContent) error {
		zap.L().Debug(fmt.Sprintf("call filter(%s) before", stack.Type), zap.Any("filter", stack))
		err := call(ctx, writer, request, metadata, parentCall)
		zap.L().Debug(fmt.Sprintf("call filter(%s) after", stack.Type), zap.Any("filter", stack), zap.Error(err))
		return err
	}
}

type NextCall func(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	metadata *PageDomainContent,
) error

var NotFountNextCall = func(ctx context.Context, writer http.ResponseWriter, request *http.Request, metadata *PageDomainContent) error {
	return os.ErrNotExist
}

type FilterCall func(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	metadata *PageDomainContent,
	next NextCall,
) error

type FilterInstance func(config FilterParams) (FilterCall, error)
