package goja

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/dop251/goja_nodejs/url"
	"gopkg.d7z.net/gitea-pages/pkg/core"
)

const internalHandlerName = "__page_internal_handler__"

func initRuntime(
	ctx core.FilterContext,
	vm *goja.Runtime,
	request *http.Request,
	global Config,
	sharedClient *http.Client,
	debug *DebugData,
	closers *Closers,
	jsLoop *eventloop.EventLoop,
) (*goja.Object, error) {
	url.Enable(vm)
	if err := installHandlerRegistration(vm); err != nil {
		return nil, err
	}
	if err := installHeaders(vm); err != nil {
		return nil, err
	}
	if err := installRequest(vm); err != nil {
		return nil, err
	}
	if err := installResponse(vm); err != nil {
		return nil, err
	}
	if err := installTextCodecs(vm); err != nil {
		return nil, err
	}
	if err := installAbortPrimitives(vm); err != nil {
		return nil, err
	}
	if err := installFrameworkHelpers(vm); err != nil {
		return nil, err
	}
	if err := installFetch(ctx, vm, jsLoop, sharedClient, global.Fetch); err != nil {
		return nil, err
	}
	_, err := installHostGlobals(ctx, vm, jsLoop, global.FS.Enabled)
	if err != nil {
		return nil, err
	}
	if global.Realtime.WebSocket {
		closer, err := installWebSocket(ctx, vm, debug, request, jsLoop)
		if err != nil {
			return nil, err
		}
		closers.AddCloser(closer.Close)
	}
	if global.Realtime.SSE {
		closer, err := installSSE(ctx, vm, debug)
		if err != nil {
			return nil, err
		}
		closers.AddCloser(closer.Close)
	}
	return newIncomingRequestObject(vm, request, global.Request.MaxBodyBytes)
}

func installHandlerRegistration(vm *goja.Runtime) error {
	return vm.Set("serve", func(handler goja.Value) error {
		if goja.IsUndefined(handler) || goja.IsNull(handler) {
			return errors.New("invalid handler")
		}
		if _, ok := goja.AssertFunction(handler); ok {
			return vm.GlobalObject().Set(internalHandlerName, handler)
		}
		obj := handler.ToObject(vm)
		if obj == nil {
			return errors.New("handler must be a function or an object with fetch(request)")
		}
		if _, ok := goja.AssertFunction(obj.Get("fetch")); !ok {
			return errors.New("handler must be a function or an object with fetch(request)")
		}
		return vm.GlobalObject().Set(internalHandlerName, handler)
	})
}

func callHandler(vm *goja.Runtime, requestObj *goja.Object) (goja.Value, error) {
	handler := vm.Get(internalHandlerName)
	if goja.IsUndefined(handler) || goja.IsNull(handler) {
		return nil, errors.New("missing handler registration; call serve(handler)")
	}
	if fn, ok := goja.AssertFunction(handler); ok {
		return fn(goja.Undefined(), requestObj)
	}
	obj := handler.ToObject(vm)
	if obj == nil {
		return nil, fmt.Errorf("invalid handler: %s", handler.String())
	}
	fetchValue := obj.Get("fetch")
	fetchFn, ok := goja.AssertFunction(fetchValue)
	if !ok {
		return nil, errors.New("handler must be a function or an object with fetch(request)")
	}
	return fetchFn(obj, requestObj)
}
