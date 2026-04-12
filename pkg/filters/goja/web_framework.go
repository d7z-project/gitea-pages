package goja

import "github.com/dop251/goja"

const frameworkBootstrap = `
(() => {
  function normalizeResponseInit(init, headers) {
    const next = init ? { ...init } : {};
    next.headers = { ...(init?.headers || {}), ...headers };
    return next;
  }

  function text(body, init) {
    return new Response(body, normalizeResponseInit(init, {
      "content-type": "text/plain; charset=utf-8",
    }));
  }

  function html(body, init) {
    return new Response(body, normalizeResponseInit(init, {
      "content-type": "text/html; charset=utf-8",
    }));
  }

  function json(data, init) {
    return Response.json(data, init);
  }

  async function read(request, kind) {
    const mode = kind || request.headers.get("content-type") || "text/plain";
    if (mode.includes("application/json") || mode === "json") {
      return await request.json();
    }
    if (mode.includes("application/x-www-form-urlencoded") || mode === "form") {
      return Object.fromEntries(new URLSearchParams(await request.text()).entries());
    }
    if (mode === "bytes" || mode.includes("application/octet-stream")) {
      return await request.arrayBuffer();
    }
    return await request.text();
  }

  function redirect(location, status) {
    return Response.redirect(location, status);
  }

  function error(status, message) {
    return text(message || "Error", { status });
  }

  function noContent() {
    return new Response(null, { status: 204 });
  }

  function notFound(message) {
    return error(404, message || "Not Found");
  }

  function methodNotAllowed(methods) {
    const allow = Array.isArray(methods) ? methods.join(", ") : methods;
    return text("Method Not Allowed", {
      status: 405,
      headers: allow ? { "allow": allow } : undefined,
    });
  }

  function parseCookieHeader(value) {
    const result = {};
    for (const item of (value || "").split(";")) {
      const part = item.trim();
      if (!part) continue;
      const idx = part.indexOf("=");
      if (idx < 0) continue;
      const key = part.slice(0, idx).trim();
      const val = part.slice(idx + 1).trim();
      if (!key) continue;
      result[key] = decodeURIComponent(val);
    }
    return result;
  }

  function cookie(request, name) {
    const cookies = parseCookieHeader(request.headers.get("cookie") || "");
    if (typeof name === "string") {
      return cookies[name] ?? null;
    }
    return cookies;
  }

  function serializeCookie(name, value, options) {
    const opts = options || {};
    const parts = [name + "=" + encodeURIComponent(value)];
    if (opts.maxAge != null) parts.push("Max-Age=" + opts.maxAge);
    if (opts.domain) parts.push("Domain=" + opts.domain);
    if (opts.path) parts.push("Path=" + opts.path);
    if (opts.expires) parts.push("Expires=" + opts.expires);
    if (opts.sameSite) parts.push("SameSite=" + opts.sameSite);
    if (opts.httpOnly) parts.push("HttpOnly");
    if (opts.secure) parts.push("Secure");
    return parts.join("; ");
  }

  async function withHeaders(response, headers) {
    const body = await response.arrayBuffer();
    const next = new Headers(response.headers);
    for (const [key, value] of Object.entries(headers || {})) {
      next.set(key, value);
    }
    return new Response(body, {
      status: response.status,
      statusText: response.statusText,
      headers: next,
    });
  }

  async function cors(response, options) {
    const opts = options || {};
    const origin = opts.origin || "*";
    const methods = opts.methods || "GET, POST, PUT, PATCH, DELETE, OPTIONS";
    const headers = opts.headers || "content-type, authorization";
    const exposeHeaders = opts.exposeHeaders;
    const credentials = opts.credentials;
    const nextHeaders = {
      "access-control-allow-origin": origin,
      "access-control-allow-methods": Array.isArray(methods) ? methods.join(", ") : methods,
      "access-control-allow-headers": Array.isArray(headers) ? headers.join(", ") : headers,
    };
    if (exposeHeaders) {
      nextHeaders["access-control-expose-headers"] = Array.isArray(exposeHeaders) ? exposeHeaders.join(", ") : exposeHeaders;
    }
    if (credentials) {
      nextHeaders["access-control-allow-credentials"] = "true";
    }
    return await withHeaders(response, nextHeaders);
  }

  async function setCookie(response, name, value, options) {
    return await withHeaders(response, {
      "set-cookie": serializeCookie(name, value, options),
    });
  }

  async function clearCookie(response, name, options) {
    const opts = { ...(options || {}), maxAge: 0, expires: "Thu, 01 Jan 1970 00:00:00 GMT" };
    return await setCookie(response, name, "", opts);
  }

  function sse() {
    return createEventStream();
  }

  function escapeRegExp(value) {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  }

  function compilePath(path) {
    if (!path || path === "*") {
      return { regexp: /^.*$/, keys: [] };
    }
    const keys = [];
    const source = path.split("/").map((segment) => {
      if (segment === "*") {
        keys.push("wildcard");
        return "(.*)";
      }
      if (segment.startsWith(":")) {
        keys.push(segment.slice(1));
        return "([^/]+)";
      }
      return escapeRegExp(segment);
    }).join("/");
    return {
      regexp: new RegExp("^" + source + "$"),
      keys,
    };
  }

  class Router {
    constructor() {
      this.routes = [];
    }

    on(method, path, handler) {
      const matcher = compilePath(path);
      this.routes.push({ method, path, handler, matcher });
      return this;
    }

    all(path, handler) { return this.on("*", path, handler); }
    get(path, handler) { return this.on("GET", path, handler); }
    post(path, handler) { return this.on("POST", path, handler); }
    put(path, handler) { return this.on("PUT", path, handler); }
    patch(path, handler) { return this.on("PATCH", path, handler); }
    delete(path, handler) { return this.on("DELETE", path, handler); }
    options(path, handler) { return this.on("OPTIONS", path, handler); }
    head(path, handler) { return this.on("HEAD", path, handler); }

    async fetch(request) {
      const url = new URL(request.url);
      const allowed = [];
      let matchedPath = false;
      for (const route of this.routes) {
        const match = route.matcher.regexp.exec(url.pathname);
        if (!match) {
          continue;
        }
        matchedPath = true;
        if (route.method !== "*" && route.method !== request.method) {
          allowed.push(route.method);
          continue;
        }
        const params = {};
        for (let i = 0; i < route.matcher.keys.length; i++) {
          params[route.matcher.keys[i]] = match[i + 1];
        }
        return await route.handler(request, { params, query: url.searchParams, url });
      }
      if (matchedPath && allowed.length > 0) {
        return methodNotAllowed([...new Set(allowed)]);
      }
      return notFound();
    }

    async handle(request) {
      return this.fetch(request);
    }
  }

  globalThis.http = {
    text,
    html,
    json,
    read,
    redirect,
    error,
    noContent,
    notFound,
    methodNotAllowed,
    cookie,
    withHeaders,
    cors,
    setCookie,
    clearCookie,
    sse,
    router() {
      return new Router();
    },
  };
})();
`

func installFrameworkHelpers(vm *goja.Runtime) error {
	_, err := vm.RunString(frameworkBootstrap)
	return err
}
