package core

import (
	"context"
	"encoding/json"
	"net/http"
)

type FilterParams map[string]any

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

func NextCallWrapper(call FilterCall, parentCall NextCall) NextCall {
	return func(ctx context.Context, writer http.ResponseWriter, request *http.Request, metadata *PageDomainContent) error {
		return call(ctx, writer, request, metadata, parentCall)
	}
}

type NextCall func(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	metadata *PageDomainContent,
) error

type FilterCall func(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	metadata *PageDomainContent,
	next NextCall,
) error

type FilterInstance func(config FilterParams) (FilterCall, error)
