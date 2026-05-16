# gitea-pages

A self-hosted Gitea Pages server with static hosting, route filters, and JavaScript handlers.

This project is part of Dragon's Zone HomeLab.

## Overview

`gitea-pages` serves content from Gitea repositories and adds a small routing layer on top of normal Pages-style hosting.

It is designed for self-hosted deployments and supports:

- static file serving from a Pages branch
- JavaScript route handlers with Goja
- reverse proxy routes
- custom domains
- private page access with Gitea OAuth
- caching, storage, and event helpers for scripts

For Chinese documentation, see [README_zh.md](./README_zh.md).

> [!WARNING]
> This project is intended for self-hosted environments. Domain ownership is not verified for page aliases.

## Getting Started

Requirements:

- Go `1.25+`
- `make`

Build:

```bash
make gitea-pages
```

Run:

```bash
./gitea-pages -conf config.yaml
```

## Configuration

- Server configuration: [config.yaml](./config.yaml)
- Page routing and security: `.pages.yaml` in the page branch
- JavaScript filter APIs: [pkg/filters/goja/README.md](./pkg/filters/goja/README.md)

## Examples

Examples are available in [examples](./examples):

- `examples/hello_world`
- `examples/js_hello_world`
- `examples/js_router`
- `examples/js_storage`
- `examples/js_ws`
- `examples/js_sse`

## Development

Run tests:

```bash
make test
```

Run formatting:

```bash
make fmt
```

Run a local example:

```bash
go run ./cmd/local/main.go -path examples/js_hello_world
```

## License

Licensed under [Apache-2.0](./LICENSE).
