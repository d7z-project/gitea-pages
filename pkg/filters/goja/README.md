# Goja Filter

`goja` filter runs JavaScript handlers for page routes.

## Entry

Register a handler with `serve(...)`.

```js
serve(async function(request) {
  return new Response("hello world")
})
```

You can also register an object with `fetch(request)`.

```js
const app = {
  async fetch(request) {
    return Response.json({ method: request.method })
  },
}

serve(app)
```

## Runtime

Available Web APIs:

- `Request`
- `Response`
- `Headers`
- `fetch`
- `URL`
- `URLSearchParams`
- `TextEncoder`
- `TextDecoder`
- `AbortController`
- `AbortSignal`

Host APIs:

- `page.meta`
- `page.auth`
- `storage.*`
- `fs.list(path?)`
- `kv.repo(...group)`
- `kv.org(...group)`
- `event.load(key)`
- `event.put(key, value)`
- `versionEvent.load(key)`
- `versionEvent.put(key, value)`
- `upgradeWebSocket(request?)`
- `http.*`

`event.*` is shared across page versions in the same repo.

`versionEvent.*` is scoped to the current page commit. Events published by one version are isolated from other versions of the same repo.

`event.load(key)` and `versionEvent.load(key)` wait for the next broadcast event on the key. They do not read a current value and they do not provide event history.

Each request keeps a live subscription per event key. If the local backlog overflows, `load(key)` rejects with `event backlog overflow`. A later `load(key)` call can establish a fresh subscription and continue receiving new events.

`storage.*` is a read-write repo-scoped filesystem rooted at the current repo. It follows a Node.js-style `fs` / `fs.promises` API with both async methods such as `storage.readFile(...)` and sync methods such as `storage.readFileSync(...)`.

`fs.*` remains a read-only view of the current page commit source tree.

`fetch` only allows `http` and `https`. When private-network blocking is enabled, fetch dials validated public IPs directly and does not use proxies.

## Helpers

`http` provides thin helpers for common handlers.

```js
const app = http.router()

app.get("/repo1/api/hello", async () => {
  return http.json({ ok: true })
})

serve(app)
```

Available helpers:

- `http.text`
- `http.html`
- `http.json`
- `http.read`
- `http.redirect`
- `http.error`
- `http.noContent`
- `http.notFound`
- `http.methodNotAllowed`
- `http.cookie`
- `http.withHeaders`
- `http.setCookie`
- `http.clearCookie`
- `http.sse`
- `http.router`

Cross-origin access, WebSocket origin checks, and cookie sharing are controlled by the page `security` config in `.pages.yaml`, not by goja helpers.

## WebSocket

```js
serve(function(request) {
  const { socket, response } = upgradeWebSocket(request)

  socket.addEventListener("message", async (event) => {
    await socket.send("ECHO: " + event.data)
  })

  return response
})
```

`socket.send(...)` accepts text, `Uint8Array`, and `ArrayBuffer`. Typed-array views use their actual `byteOffset` and `byteLength`.

## SSE

```js
serve(function() {
  const { stream, response } = http.sse()

  void (async () => {
    await stream.send("hello", { event: "message", id: "1" })
    stream.close()
  })()

  return response
})
```

## Types

Type definitions are published from [`global-types`](../../../global-types/package.json).

Install:

```bash
npm install -D @d7z-project/gitea-pages
```

Then include the package types in `tsconfig.json`:

```json
{
  "compilerOptions": {
    "types": ["@d7z-project/gitea-pages"]
  }
}
```

Or reference them from a script file:

```ts
/// <reference types="@d7z-project/gitea-pages" />
```

## Examples

See:

- [examples/js_hello_world](../../../examples/js_hello_world)
- [examples/js_kv](../../../examples/js_kv)
- [examples/js_storage](../../../examples/js_storage)
- [examples/js_ws](../../../examples/js_ws)
- [examples/js_ws_event](../../../examples/js_ws_event)
- [examples/js_sse](../../../examples/js_sse)
