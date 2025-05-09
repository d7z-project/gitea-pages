# gitea-pages

> Next-generation Gitea Pages, replacing the previous caddy-gitea-proxy

**This project is part of Dragon's Zone HomeLab**

This project focuses on functional implementation and does not consider any performance optimizations or large-scale deployment scenarios. Any issues arising from this are not related to the project.

**Note**: The project recently added custom renderers and reverse proxy functionality, which may lead to serious security and performance issues. If not needed, it can be turned off in the settings.

## Get Started

Install `go1.24` or later, along with the `Make` tool, and then execute the following command:

```bash
make gitea-pages
```

After that, you can start it using the following command:

```bash
./gitea-pages -conf config.yaml
```

For specific configurations, check [`config.yaml`](./config.yaml).

### Page Config

Create `.pages.yaml` in the project's default branch and fill in the following content:

```yaml
v-route: true # Virtual routing
alias: # CNAME
  - "example.com"
  - "example2.com"
templates: # Renderer
  gotemplate: '**/*.tmpl,**/index.html'
proxy:
  /api: https://github.com/api
ignore: .git/**,.pages.yaml
```

## TODO

- [x] Content caching
- [x] CNAME custom domains
- [x] Template rendering
- [x] Reverse proxy requests
- [ ] OAuth2 authorized access to private pages
- [ ] ~~http01 auto-certificate issuance~~: Handled by Caddy
- [ ] ~~Webhook-triggered updates~~: Not a high priority for real-time needs

## LICENSE

This project is licensed under [Apache-2.0](./LICENSE)