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
    strip_request_headers:
      - Authorization
      - Cookie
      - Forwarded
      - Proxy-Authorization
      - X-Forwarded-For
      - X-Forwarded-Host
      - X-Forwarded-Proto
      - X-Page-Host
      - X-Page-IP
      - X-Page-Refer
      - X-Real-IP
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
```

## TODO

- [ ] Support CORS
- [ ] Support custom caching strategies (HTTP cache-control)
- [ ] ~~http01 automatic certificate issuance~~: Handled by Caddy
- [ ] ~~Web hook triggers for updates~~: Not a high priority for real-time needs

## LICENSE

This project is licensed under [Apache-2.0](./LICENSE)
