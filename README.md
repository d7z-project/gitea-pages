# gitea-pages

> 新一代 Gitea Pages，替换之前的 caddy-gitea-proxy

**此项目是 Dragon's Zone HomeLab 的一部分**

本项目的侧重于功能实现，并未考虑任何性能优化，亦未考虑大规模部署的情况，由此带来的任何问题与项目无关。

注意，项目在最近加入了自定义渲染器功能，可能导致严重的安全和性能问题，如出现相关问题请反馈。

## Get Started

安装 `go1.23` 或更高版本，同时安装 `Make` 工具 ，然后执行如下命令:

```bash
make gitea-pages
```

之后可使用如下命令启动

```bash
./gitea-pages -conf config.yaml
```

具体配置可查看 [`config.yaml`](./config.yaml)。

### Render

说明: **不会**将文件系统 引入到渲染器中，复杂的渲染流程应该采用更加灵活轻便的方案

在项目的根目录创建 `.render` 文件，填入如下内容:

```sh
#  parser       match
gotemplate     **/*.tmpl
```
其中，`gotemplate` 为解析器类型，`**/*.tmpl` 为匹配的路径，使用 `github.com/gobwas/glob`

## TODO

- [x] 内容缓存
- [x] CNAME 自定义域名
- [ ] OAuth2 授权访问私有页面
- [ ] ~~http01 自动签发证书~~: 交由 Caddy 完成
- [ ] ~~Web 钩子触发更新~~: 对实时性需求不大

## LICENSE

此项目使用 [Apache-2.0](./LICENSE)