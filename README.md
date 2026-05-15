# gitea-pages

> The next generation Gitea Pages, replacing the previous `caddy-gitea-proxy`

**This project is part of Dragon's Zone HomeLab**

This project focuses on providing a high-performance, secure, and extensible Gitea Pages service.

**Note**: This project has been completely refactored and is not compatible with upgrades from version `0.0.1`

## Features

- [x] **High Performance**: Optimized JS execution with program caching and efficient promise handling.
- [x] **Secure**: Built-in path traversal protection for template and file access.
- [x] **Content Caching**: Multi-level caching for metadata and blob content.
- [x] **CNAME**: Support for custom domains.
- [x] **Template Engine**: Secure template rendering with include support.
- [x] **Programmable**: Extensible logic using JavaScript (Goja).
- [x] **Reverse Proxy**: Support for proxying requests to backends.
- [x] OAuth2 authorization for accessing private pages

> [!WARNING]
> **Security Note**: This project is designed for self-hosted/private environments. It does not perform domain ownership verification for CNAME aliases. In a multi-user environment, users could potentially "hijack" domains by claiming them in their `.pages.yaml`.


## Get Started

Install `go1.25` or higher, and also install the `Make` tool, then execute the following command:

```bash
make gitea-pages
```

After that, you can start it with the following command:

```bash
./gitea-pages -conf config.yaml
```

For specific configurations, see [`config.yaml`](./config.yaml).

### Reverse Proxy Setup

If `gitea-pages` is behind Caddy, Nginx, Traefik, or an ingress controller, set `trusted_proxies` in [`config.yaml`](./config.yaml) to the proxy egress IPs or CIDRs. Only requests from those addresses are allowed to supply `X-Forwarded-For` and `X-Forwarded-Proto`.

Example:

```yaml
trusted_proxies:
  - 127.0.0.1/32
  - 10.0.0.0/8
```

The `reverse_proxy` route filter is enabled by default and can be configured globally under `filters.reverse_proxy`:

```yaml
filters:
  reverse_proxy:
    enabled: true
    forward_authorization: false
```

Per-page route example in `.pages.yaml`:

```yaml
routes:
  - path: "/api/**"
    reverse_proxy:
      prefix: /api
      target: https://example-upstream.com
```

Notes:

- `target` must be an absolute `https://` URL.
- Targets that resolve to loopback, private, or link-local addresses are rejected.
- `prefix` is removed from the matched request path before proxying.
- `Forwarded`, `X-Forwarded-*`, `X-Real-IP`, and `X-Page-*` are always rebuilt by the proxy filter.
- Cookie forwarding is controlled by the page `security` config.
- `Authorization` is dropped by default. Set `forward_authorization: true` only when the upstream explicitly needs it.

## JavaScript Filter

Goja filter usage, host APIs, and TypeScript global types are documented in [pkg/filters/goja/README.md](./pkg/filters/goja/README.md).

### Page Config

Create a `.pages.yaml` file in the `gh-pages` branch of your project and fill in the following content:

```yaml
alias: # CNAME
  - "example.com"
  - "example2.com"
routes:
  - path: "**"
    js:
      exec: index.js
security:
  cors:
    origins:
      - "https://app.example.com"
    credentials: true
  cookies:
    require_https: true
    allow_cross_origin: false
  headers:
    cross_origin_resource_policy: same-origin
```

`security` is page-level and applies to direct/static responses, Goja routes, WebSocket upgrades, auth routes, and `reverse_proxy` routes for that page.

Defaults:

- Cross-origin requests are rejected unless `security.cors.origins` explicitly allows the request `Origin`.
- Cookies are disabled on `http`.
- Cross-origin cookies are disabled unless both `security.cookies.allow_cross_origin` and `security.cors.credentials` allow them.
- `Cross-Origin-Resource-Policy` defaults to `same-origin`.

Fields:

- `security.cors.origins`: allowed cross-origin origins. Empty means same-origin only.
- `security.cors.methods`: allowed methods returned in preflight responses. Default: `GET, POST, PUT, PATCH, DELETE, OPTIONS`.
- `security.cors.headers`: allowed request headers returned in preflight responses. Default: `content-type, authorization`.
- `security.cors.expose`: response headers exposed to browsers.
- `security.cors.credentials`: enables `Access-Control-Allow-Credentials` for allowed cross-origin requests.
- `security.cors.max_age`: preflight cache time in seconds. Default: `600`.
- `security.cookies.enabled`: enables cookie handling for the page. Default: `true`.
- `security.cookies.require_https`: strips request `Cookie` and response `Set-Cookie` on `http`. Default: `true`.
- `security.cookies.allow_cross_origin`: allows cross-origin cookies when CORS credentials are also enabled. Default: `false`.
- `security.headers.cross_origin_resource_policy`: value for `Cross-Origin-Resource-Policy`. Default: `same-origin`.
- `security.headers.frame_options`: optional `X-Frame-Options` value.

## TODO

- [x] Support CORS
- [ ] Support custom caching strategies (HTTP cache-control)
- [ ] ~~http01 automatic certificate issuance~~: Handled by Caddy
- [ ] ~~Web hook triggers for updates~~: Not a high priority for real-time needs

## LICENSE

This project is licensed under [Apache-2.0](./LICENSE)
