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
- `fs.list(path?)`
- `kv.repo(...group)`
- `kv.org(...group)`
- `event.load(key)`
- `event.put(key, value)`
- `upgradeWebSocket(request?)`
- `http.*`

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
- `http.cors`
- `http.setCookie`
- `http.clearCookie`
- `http.sse`
- `http.router`

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
- [examples/js_ws](../../../examples/js_ws)
- [examples/js_ws_event](../../../examples/js_ws_event)
- [examples/js_sse](../../../examples/js_sse)
