package goja

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	pkgerrors "github.com/pkg/errors"
	"gopkg.d7z.net/gitea-pages/pkg/core"
	"gopkg.d7z.net/middleware/kv"
	"gopkg.d7z.net/middleware/subscribe"
)

const defaultEventPendingLimit = 256

var errEventBacklogOverflow = errors.New("event backlog overflow")

type eventResult struct {
	value string
	err   error
}

type eventStream struct {
	mu       sync.Mutex
	pending  []string
	waiters  []chan eventResult
	closed   error
	limit    int
	overflow bool
}

func installHostGlobals(
	ctx core.FilterContext,
	vm *goja.Runtime,
	loop *eventloop.EventLoop,
	eventPendingLimit int,
	runtime *runtimeState,
	closers *Closers,
) (*goja.Object, error) {
	if eventPendingLimit <= 0 {
		eventPendingLimit = defaultEventPendingLimit
	}

	host := vm.NewObject()
	if err := host.Set("meta", map[string]any{
		"org":    ctx.Owner,
		"repo":   ctx.Repo,
		"commit": ctx.CommitID,
	}); err != nil {
		return nil, err
	}
	if err := host.Set("auth", map[string]any{
		"authenticated": ctx.Auth.Authenticated,
		"identity":      authIdentityMap(ctx.Auth.Identity),
	}); err != nil {
		return nil, err
	}
	if err := vm.Set("fs", map[string]any{
		"list": func(path ...string) (goja.Value, error) {
			target := ""
			if len(path) > 0 {
				target = path[0]
			}
			list, err := ctx.PageVFS.List(ctx, target)
			if err != nil {
				return nil, err
			}
			items := make([]map[string]any, len(list))
			for i, item := range list {
				items[i] = map[string]any{
					"name": item.Name,
					"path": item.Path,
					"type": item.Type,
					"size": item.Size,
				}
			}
			return vm.ToValue(items), nil
		},
		"read": func(path string) *goja.Promise {
			promise, resolve, reject := vm.NewPromise()
			if !runtime.startTask() {
				_ = reject(vm.ToValue(errRuntimeClosing))
				return promise
			}
			go func() {
				defer runtime.finishTask()
				data, err := ctx.PageVFS.Read(ctx, path)
				runtime.runOnLoop(loop, func(vm *goja.Runtime) {
					if err != nil {
						_ = reject(vm.ToValue(err))
						return
					}
					_ = resolve(uint8ArrayValue(vm, data))
				})
			}()
			return promise
		},
		"readText": func(path string) *goja.Promise {
			promise, resolve, reject := vm.NewPromise()
			if !runtime.startTask() {
				_ = reject(vm.ToValue(errRuntimeClosing))
				return promise
			}
			go func() {
				defer runtime.finishTask()
				data, err := ctx.PageVFS.ReadString(ctx, path)
				runtime.runOnLoop(loop, func(vm *goja.Runtime) {
					if err != nil {
						_ = reject(vm.ToValue(err))
						return
					}
					_ = resolve(vm.ToValue(data))
				})
			}()
			return promise
		},
		"readSync": func(path string) (goja.Value, error) {
			data, err := ctx.PageVFS.Read(ctx, path)
			if err != nil {
				return nil, err
			}
			return uint8ArrayValue(vm, data), nil
		},
		"readTextSync": func(path string) (string, error) {
			return ctx.PageVFS.ReadString(ctx, path)
		},
		"openReadable": func(path string, options ...goja.Value) (*goja.Object, error) {
			offset := int64(0)
			if len(options) > 0 && !isNilish(options[0]) {
				obj, ok := valueObject(vm, options[0])
				if !ok {
					return nil, errors.New("invalid read options")
				}
				if value, ok := objectInt64(obj, "offset"); ok && value > 0 {
					offset = value
				}
			}
			stream := &readableStreamState{
				open: func() (io.ReadCloser, error) {
					reader, err := ctx.PageVFS.Open(ctx, path)
					if err != nil {
						return nil, err
					}
					if offset == 0 {
						return reader, nil
					}
					seeker, ok := reader.(io.Seeker)
					if !ok {
						_ = reader.Close()
						return nil, errors.New("stream offset is not supported by this fs backend")
					}
					if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
						_ = reader.Close()
						return nil, err
					}
					return reader, nil
				},
			}
			closers.AddCloser(stream.close)
			return newReadableStreamObject(vm, loop, runtime, stream), nil
		},
	}); err != nil {
		return nil, err
	}
	if err := vm.Set("storage", newStorageAPI(vm, loop, runtime, closers, ctx.Storage)); err != nil {
		return nil, err
	}
	if err := vm.Set("kv", map[string]any{
		"repo": func(group ...string) (goja.Value, error) {
			return kvResult(ctx.RepoDB)(ctx, vm, group...)
		},
		"org": func(group ...string) (goja.Value, error) {
			return kvResult(ctx.OrgDB)(ctx, vm, group...)
		},
	}); err != nil {
		return nil, err
	}

	sharedStreams := make(map[string]*eventStream)
	versionStreams := make(map[string]*eventStream)
	var sharedMu sync.Mutex
	var versionMu sync.Mutex

	if err := vm.Set("event", newEventAPI(ctx, vm, loop, runtime, &sharedMu, sharedStreams, ctx.SharedEvent.Subscribe, ctx.SharedEvent.Publish, eventPendingLimit)); err != nil {
		return nil, err
	}
	if err := vm.Set("versionEvent", newEventAPI(ctx, vm, loop, runtime, &versionMu, versionStreams, ctx.VersionEvent.Subscribe, ctx.VersionEvent.Publish, eventPendingLimit)); err != nil {
		return nil, err
	}
	if err := vm.Set("page", host); err != nil {
		return nil, err
	}
	return host, nil
}

func newEventAPI(
	ctx context.Context,
	vm *goja.Runtime,
	loop *eventloop.EventLoop,
	runtime *runtimeState,
	streamsMu *sync.Mutex,
	streams map[string]*eventStream,
	subscribeFn func(context.Context, string) (subscribe.Subscription, error),
	publishFn func(context.Context, string, string) error,
	limit int,
) map[string]any {
	ensureStream := func(key string) (*eventStream, error) {
		streamsMu.Lock()
		if stream := streams[key]; stream != nil && !stream.isClosed() {
			streamsMu.Unlock()
			return stream, nil
		}
		delete(streams, key)
		sub, err := subscribeFn(ctx, key)
		if err != nil {
			streamsMu.Unlock()
			return nil, err
		}
		stream := &eventStream{limit: limit}
		streams[key] = stream
		streamsMu.Unlock()

		go superviseEventStream(ctx, key, streamsMu, streams, subscribeFn, stream, sub)
		return stream, nil
	}

	return map[string]any{
		"load": func(key string) *goja.Promise {
			promise, resolve, reject := vm.NewPromise()
			stream, err := ensureStream(key)
			if err != nil {
				_ = reject(vm.ToValue(err))
				return promise
			}
			// event is a broadcast stream. load(key) means "wait for the next
			// broadcast event on this key", not "read the current value".
			// Each request/key keeps one live subscription so repeated
			// `while (true) await event.load(key)` calls do not lose broadcasts
			// between iterations.
			stream.await(loop, vm, resolve, reject, runtime)
			return promise
		},
		"put": func(key, value string) *goja.Promise {
			promise, resolve, reject := vm.NewPromise()
			if !runtime.startTask() {
				_ = reject(vm.ToValue(errRuntimeClosing))
				return promise
			}
			go func() {
				defer runtime.finishTask()
				err := publishFn(ctx, key, value)
				runtime.runOnLoop(loop, func(vm *goja.Runtime) {
					if err != nil {
						_ = reject(vm.ToValue(err))
						return
					}
					_ = resolve(goja.Undefined())
				})
			}()
			return promise
		},
	}
}

func superviseEventStream(
	ctx context.Context,
	key string,
	streamsMu *sync.Mutex,
	streams map[string]*eventStream,
	subscribeFn func(context.Context, string) (subscribe.Subscription, error),
	stream *eventStream,
	initial subscribe.Subscription,
) {
	defer func() {
		streamsMu.Lock()
		if streams[key] == stream {
			delete(streams, key)
		}
		streamsMu.Unlock()
	}()

	backoff := 50 * time.Millisecond
	sub := initial
	for {
		if stream.isClosed() {
			return
		}
		if ctx.Err() != nil {
			stream.finish(ctx.Err())
			return
		}
		if sub == nil {
			next, err := subscribeFn(ctx, key)
			if err != nil {
				if !waitEventRetry(ctx, backoff) {
					stream.finish(ctx.Err())
					return
				}
				if backoff < time.Second {
					backoff *= 2
				}
				continue
			}
			sub = next
		}
		backoff = 50 * time.Millisecond
		if !consumeEventSubscription(ctx, sub, stream) {
			return
		}
		sub = nil
		if !waitEventRetry(ctx, backoff) {
			stream.finish(ctx.Err())
			return
		}
		if backoff < time.Second {
			backoff *= 2
		}
	}
}

func consumeEventSubscription(ctx context.Context, sub subscribe.Subscription, stream *eventStream) bool {
	defer sub.Close()
	for {
		if stream.isClosed() {
			return false
		}
		select {
		case event, ok := <-sub.Events():
			if !ok {
				return true
			}
			stream.push(event.Value)
		case _, ok := <-sub.Errors():
			if !ok {
				return true
			}
			return true
		case <-ctx.Done():
			stream.finish(ctx.Err())
			return false
		}
	}
}

func waitEventRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *eventStream) push(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed != nil {
		return
	}
	if len(s.waiters) > 0 {
		waiter := s.waiters[0]
		s.waiters = s.waiters[1:]
		waiter <- eventResult{value: value}
		close(waiter)
		return
	}
	if len(s.pending) >= s.limit {
		s.overflow = true
		return
	}
	s.pending = append(s.pending, value)
}

func (s *eventStream) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed != nil
}

func (s *eventStream) finish(err error) {
	if err == nil {
		err = context.Canceled
	}
	s.mu.Lock()
	if s.closed != nil {
		s.mu.Unlock()
		return
	}
	s.closed = err
	waiters := s.waiters
	s.waiters = nil
	s.pending = nil
	s.mu.Unlock()
	for _, waiter := range waiters {
		waiter <- eventResult{err: err}
		close(waiter)
	}
}

func (s *eventStream) await(
	loop *eventloop.EventLoop,
	vm *goja.Runtime,
	resolve func(any) error,
	reject func(any) error,
	runtime *runtimeState,
) {
	waiter := make(chan eventResult, 1)
	s.mu.Lock()
	switch {
	case len(s.pending) > 0:
		value := s.pending[0]
		s.pending = s.pending[1:]
		s.mu.Unlock()
		_ = resolve(vm.ToValue(value))
		return
	case s.overflow:
		s.overflow = false
		s.mu.Unlock()
		s.finish(errEventBacklogOverflow)
		_ = reject(vm.ToValue(errEventBacklogOverflow))
		return
	case s.closed != nil:
		err := s.closed
		s.mu.Unlock()
		_ = reject(vm.ToValue(err))
		return
	default:
		s.waiters = append(s.waiters, waiter)
		s.mu.Unlock()
	}
	if !runtime.startTask() {
		s.mu.Lock()
		for i, current := range s.waiters {
			if current == waiter {
				s.waiters = append(s.waiters[:i], s.waiters[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		_ = reject(vm.ToValue(errRuntimeClosing))
		return
	}
	go func() {
		defer runtime.finishTask()
		result := <-waiter
		runtime.runOnLoop(loop, func(vm *goja.Runtime) {
			if result.err != nil {
				_ = reject(vm.ToValue(result.err))
				return
			}
			_ = resolve(vm.ToValue(result.value))
		})
	}()
}

func authIdentityMap(identity *core.AuthIdentity) any {
	if identity == nil {
		return nil
	}
	return map[string]any{
		"subject": identity.Subject,
		"name":    identity.Name,
	}
}

func kvResult(db kv.KV) func(ctx core.FilterContext, jsCtx *goja.Runtime, group ...string) (goja.Value, error) {
	return func(ctx core.FilterContext, jsCtx *goja.Runtime, group ...string) (goja.Value, error) {
		if len(group) == 0 {
			return goja.Undefined(), errors.New("invalid group")
		}
		db := db.Child(group...)
		return jsCtx.ToValue(map[string]any{
			"get": func(key string) (goja.Value, error) {
				get, err := db.Get(ctx, key)
				if err != nil {
					if !pkgerrors.Is(err, os.ErrNotExist) {
						return nil, err
					}
					return goja.Null(), nil
				}
				return jsCtx.ToValue(get), nil
			},
			"set": func(key, value string, ttl ...int) error {
				t := time.Duration(kv.TTLKeep)
				if len(ttl) > 0 && ttl[0] > 0 {
					t = time.Duration(ttl[0]) * time.Millisecond
				}
				return db.Put(ctx, key, value, t)
			},
			"delete": func(key string) (bool, error) {
				return db.Delete(ctx, key)
			},
			"putIfNotExists": func(key, value string, ttl ...int) (bool, error) {
				t := time.Duration(kv.TTLKeep)
				if len(ttl) > 0 && ttl[0] > 0 {
					t = time.Duration(ttl[0]) * time.Millisecond
				}
				return db.PutIfNotExists(ctx, key, value, t)
			},
			"compareAndSwap": func(key, oldValue, newValue string) (bool, error) {
				return db.CompareAndSwap(ctx, key, oldValue, newValue)
			},
			"list": func(limit int64, cursor string) (map[string]any, error) {
				if limit <= 0 {
					limit = 100
				}
				list, err := db.ListCurrentCursor(ctx, &kv.ListOptions{Limit: limit, Cursor: cursor})
				if err != nil {
					return nil, err
				}
				keys := make([]string, len(list.Pairs))
				items := make([]map[string]string, len(list.Pairs))
				for i, p := range list.Pairs {
					keys[i] = p.Key
					items[i] = map[string]string{"key": p.Key, "value": p.Value}
				}
				return map[string]any{
					"keys":    keys,
					"items":   items,
					"cursor":  list.Cursor,
					"hasNext": list.HasMore,
				}, nil
			},
		}), nil
	}
}
