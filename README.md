# gitea-pages

> 新一代 Gitea Pages，替换之前的 caddy-gitea-proxy

**此项目是 Dragon's Zone HomeLab 的一部分**

本项目的侧重于功能实现，并未考虑任何性能优化，亦未考虑大规模部署的情况，由此带来的任何问题与项目无关。

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

## TODO

- [x] 内容缓存
- [x] CNAME 自定义域名
- [ ] OAuth2 授权访问私有页面
- [ ] ~~http01 自动签发证书~~: 交由 Caddy 完成
- [ ] ~~Web 钩子触发更新~~: 对实时性需求不大

## LICENSE

此项目使用 [Apache-2.0](./LICENSE)