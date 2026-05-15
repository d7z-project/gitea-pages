# gitea-pages

> 新一代 Gitea Pages，替换之前的 `caddy-gitea-proxy`

**此项目是 Dragon's Zone HomeLab 的一部分**

本项目致力于提供高性能、安全且可扩展的 Gitea Pages 服务。

**注意**：此项目已经被完全重构，不兼容 `0.0.1` 版本升级

## 特性

- [x] **高性能**: 采用 JS 预编译缓存和高效的异步 Promise 处理机制。
- [x] **安全**: 内置模板和文件访问的路径穿越保护。
- [x] **内容缓存**: 多级元数据和二进制内容缓存。
- [x] **CNAME**: 支持自定义域名。
- [x] **模板引擎**: 安全的模板渲染，支持 `load` 动态加载。
- [x] **可编程**: 使用 JavaScript (Goja) 编写自定义路由逻辑。
- [x] **反向代理**: 支持将请求代理到后端服务。
- [x] OAuth2 授权访问私有页面

> [!WARNING]
> **安全提示**: 本项目设计用于自托管或私有环境。它不对 CNAME 别名进行域名所有权验证。在多用户环境中，用户可能会通过在 `.pages.yaml` 中声明他人域名来实施“劫持”。


## Get Started

安装 `go1.25` 或更高版本，同时安装 `Make` 工具 ，然后执行如下命令:

```bash
make gitea-pages
```

之后可使用如下命令启动

```bash
./gitea-pages -conf config.yaml
```

具体配置可查看 [`config.yaml`](./config.yaml)。

### 反向代理配置

如果 `gitea-pages` 前面还有 Caddy、Nginx、Traefik 或 ingress，需要在 [`config.yaml`](./config.yaml) 中配置 `trusted_proxies`，声明哪些代理出口 IP 或 CIDR 可以被信任。只有来自这些地址的请求，程序才会信任 `X-Forwarded-For` 与 `X-Forwarded-Proto`。

示例：

```yaml
trusted_proxies:
  - 127.0.0.1/32
  - 10.0.0.0/8
```

`reverse_proxy` 路由过滤器默认启用，也可以在 `filters.reverse_proxy` 下做全局配置：

```yaml
filters:
  reverse_proxy:
    enabled: true
    forward_authorization: false
    deny_hosts:
      - metadata.internal.example
    deny_cidrs:
      - 169.254.169.254/32
      - 127.0.0.0/8
```

`.pages.yaml` 中的路由示例：

```yaml
routes:
  - path: "/api/**"
    reverse_proxy:
      prefix: /api
      target: https://example-upstream.com
```

说明：

- `target` 必须是绝对 `https://` URL。
- `deny_hosts` 用于精确拒绝目标主机名。
- `deny_cidrs` 会同时匹配字面量目标 IP 和 DNS 解析得到的 IP。
- 代理会在每次请求时重新解析目标地址，按 `deny_cidrs` 过滤后，再按解析结果顺序依次尝试剩余 IP。
- 转发时会先从匹配路径中裁掉 `prefix`。
- `Forwarded`、`X-Forwarded-*`、`X-Real-IP` 和 `X-Page-*` 会始终由代理过滤器重建。
- Cookie 是否继续转发由页面 `security` 配置统一控制。
- 默认不会透传 `Authorization`；只有在上游明确需要时，才应设置 `forward_authorization: true`。

## JavaScript Filter

Goja filter 的使用方式、宿主 API 与 TypeScript 全局类型可查看 [pkg/filters/goja/README.md](./pkg/filters/goja/README.md)。

### Page Config

在项目的 `gh-pages` 分支创建 `.pages.yaml`,填入如下内容

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

`security` 是页面级配置，会统一作用于该页面的 direct/static 响应、Goja 路由、WebSocket 升级、认证路由和 `reverse_proxy` 路由。

默认行为：

- 未在 `security.cors.origins` 中显式允许时，跨域请求会被拒绝。
- `http` 下默认禁用 Cookie。
- 只有同时允许 `security.cookies.allow_cross_origin` 和 `security.cors.credentials` 时，才允许跨域 Cookie。
- `Cross-Origin-Resource-Policy` 默认为 `same-origin`。

字段说明：

- `security.cors.origins`：允许的跨域来源。为空时表示只允许同源。
- `security.cors.methods`：预检响应返回的允许方法。默认：`GET, POST, PUT, PATCH, DELETE, OPTIONS`。
- `security.cors.headers`：预检响应返回的允许请求头。默认：`content-type, authorization`。
- `security.cors.expose`：暴露给浏览器的响应头。
- `security.cors.credentials`：对已允许的跨域请求开启 `Access-Control-Allow-Credentials`。
- `security.cors.max_age`：预检缓存秒数。默认：`600`。
- `security.cookies.enabled`：是否启用该页面的 Cookie 处理。默认：`true`。
- `security.cookies.require_https`：在 `http` 下移除请求 `Cookie` 和响应 `Set-Cookie`。默认：`true`。
- `security.cookies.allow_cross_origin`：允许跨域 Cookie；仍需同时开启 CORS credentials。默认：`false`。
- `security.headers.cross_origin_resource_policy`：`Cross-Origin-Resource-Policy` 响应头值。默认：`same-origin`。
- `security.headers.frame_options`：可选的 `X-Frame-Options` 响应头值。

## TODO

- [x] 支持跨域
- [ ] 支持自定义缓存策略 (http cache-control)
- [ ] ~~http01 自动签发证书~~: 交由 Caddy 完成
- [ ] ~~Web 钩子触发更新~~: 对实时性需求不大

## LICENSE

此项目使用 [Apache-2.0](./LICENSE)
