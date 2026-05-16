package goja

import (
	"net/http"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/buffer"
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
	runtime *runtimeState,
) (*goja.Object, error) {
	buffer.Enable(vm)
	url.Enable(vm)
	if err := vm.Set("serve", func(handler goja.Value) error {
		if _, _, err := resolveHandler(vm, handler); err != nil {
			return err
		}
		return vm.GlobalObject().Set(internalHandlerName, handler)
	}); err != nil {
		return nil, err
	}
	if err := installHeaders(vm); err != nil {
		return nil, err
	}
	if err := installRequest(vm, jsLoop, runtime); err != nil {
		return nil, err
	}
	if err := installResponse(vm, jsLoop, runtime); err != nil {
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
	if err := installResponseStream(ctx, vm, debug, jsLoop, runtime, closers); err != nil {
		return nil, err
	}
	if err := installFetch(ctx, vm, jsLoop, sharedClient, global.Fetch, runtime, closers); err != nil {
		return nil, err
	}
	_, err := installHostGlobals(ctx, vm, jsLoop, global.Realtime.EventBuffer, runtime, closers)
	if err != nil {
		return nil, err
	}
	closer, err := installWebSocket(ctx, vm, debug, request, jsLoop, runtime)
	if err != nil {
		return nil, err
	}
	closers.AddCloser(closer.Close)
	return newIncomingRequestObject(vm, jsLoop, runtime, request, global.Request.MaxBodyBytes, closers)
}
