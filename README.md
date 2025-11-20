# gitea-pages

> The next generation Gitea Pages, replacing the previous `caddy-gitea-proxy`

**This project is part of Dragon's Zone HomeLab**

This project focuses on functionality implementation and does not consider any performance optimizations or large-scale deployment scenarios. Any issues arising from this are not related to the project.

**Note**: This project has been completely refactored and is not compatible with upgrades from version `0.0.1`

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

- [x] Content caching
- [x] CNAME custom domains
- [x] Template rendering
- [x] Reverse proxy requests
- [ ] Support CORS
- [ ] Support custom caching strategies (HTTP cache-control)
- [ ] OAuth2 authorization for accessing private pages
- [ ] ~~http01 automatic certificate issuance~~: Handled by Caddy
- [ ] ~~Web hook triggers for updates~~: Not a high priority for real-time needs

## LICENSE

This project is licensed under [Apache-2.0](./LICENSE)