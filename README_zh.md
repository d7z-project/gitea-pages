# gitea-pages

一个自托管的 Gitea Pages 服务，支持静态站点、路由过滤器和 JavaScript 处理逻辑。

此项目是 Dragon's Zone HomeLab 的一部分。

## 概览

`gitea-pages` 从 Gitea 仓库提供页面内容，并在常规 Pages 托管之上增加了一层轻量路由能力。

适合自托管场景，支持：

- 基于 Pages 分支的静态文件托管
- 基于 Goja 的 JavaScript 路由处理
- 反向代理路由
- 自定义域名
- 基于 Gitea OAuth 的私有页面访问
- 面向脚本的缓存、存储和事件能力

英文说明见 [README.md](./README.md)。

> [!WARNING]
> 本项目面向自托管环境，不对页面别名做域名所有权校验。

## 快速开始

环境要求：

- Go `1.25+`
- `make`

构建：

```bash
make gitea-pages
```

运行：

```bash
./gitea-pages -conf config.yaml
```

## 配置

- 服务端配置见 [config.yaml](./config.yaml)
- 页面路由与安全配置写在页面分支中的 `.pages.yaml`
- JavaScript Filter API 见 [pkg/filters/goja/README.md](./pkg/filters/goja/README.md)

## 示例

示例目录见 [examples](./examples)：

- `examples/hello_world`
- `examples/js_hello_world`
- `examples/js_router`
- `examples/js_storage`
- `examples/js_ws`
- `examples/js_sse`

## 开发

运行测试：

```bash
make test
```

格式化代码：

```bash
make fmt
```

运行本地示例：

```bash
go run ./cmd/local/main.go -path examples/js_hello_world
```

## 许可证

项目使用 [Apache-2.0](./LICENSE) 许可证。
