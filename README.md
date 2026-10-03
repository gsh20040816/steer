# Steer

Steer 是一个透明代理的配置和管理工具，底层用 sing-box 转发流量。你在 Steer 里配置节点、路由、DNS 和分流规则，Steer 把它们检查一遍、翻译成 sing-box 配置，再负责在系统上启用。

代码分两层：共享的 Go 核心负责配置格式（项目里叫 Canonical Intent）、校验、编译、应用流程、订阅和测速；各平台的适配器负责网卡、防火墙和系统服务这些和操作系统打交道的部分。

当前稳定版是 **0.11.6**，配置格式版本是 **schema 9**。支持三个平台：

- **OpenWrt 25.12.5 x86/64**：提供 APK 软件源。
- **Linux（systemd）**：提供 x86_64 和 aarch64 的通用 tar.zst 包。
- **macOS（Apple Silicon）**：提供 DMG，内含 SwiftUI 图形界面。首次安装时输入一次管理员密码，之后保存和应用都不再需要密码。不需要 Apple 开发者账号。

## 功能

- **分流规则**：按顺序匹配，命中第一条就停。可以按域名、GeoSite、目标 IP、GeoIP、来源网段、来源 MAC、网络类型、协议、端口和本地代理入口匹配。
- **路由**：直连、拒绝，或者走某个节点。走节点的路由可以再套一层前置代理（链式代理），层数不限，但不能绕成环。拒绝路由在配置里写作 `kind: block`（为了兼容旧配置），实际生成的是 sing-box 的 `reject`。
- **节点类型**：SOCKS、HTTP、Shadowsocks、VMess、VLESS、Trojan、Hysteria、ShadowTLS、TUIC、Hysteria2、AnyTLS、SSH、NaiveProxy，以及本机 Tor。
- **DNS**：支持 UDP、TCP、DoT、DoH、DoQ、DoH3。每一对实际用到的「DNS 配置 + 路由」都有自己独立的查询通道。
- **订阅**：支持 HTTP(S) 订阅。节点 ID 在多次更新之间保持不变；订阅里消失的节点如果没有被路由引用，会被自动清理，被引用的会保留并标记为过期。
- **测试**：可以测试当前网络能否访问三个可配置的目标网址，也可以单独测试某个节点或整条路由链。
- **IPv6 内核直连（可选）**：如果能根据规则提前确定某个 IPv6 流量一定走直连，就直接交给内核处理，不经过 sing-box。确定不了的仍走完整的规则匹配。
- **OpenWrt**：用 UCI 存配置、LuCI 做界面、procd 管服务。流量由 sing-box TUN 接管，源 MAC 规则用 sing-box 1.14 的原生能力实现，nftables 只额外加了一小段 DNS 转发规则。
- **Linux**：配置存成 JSON 文件，流量由 sing-box TUN 接管；nftables 规则同时处理本机和虚拟机/Docker 的 DNS，并在 nftables 服务重启后自动恢复；自带一个只监听本机地址的网页管理界面。
- **macOS**：图形界面直接编辑配置。保存和应用通过一个以 root 身份运行的后台服务完成，这个服务只接受本机管理员组用户的请求。流量由单独的 LaunchDaemon 和 sing-box TUN 处理。
- **三个界面风格统一**：LuCI、Linux 网页和 macOS 应用各自保留平台习惯，但字段、选项和「状态 / 配置 / 服务 / 高级」的页面结构都来自同一份自动生成的界面描述文件。
- **Geo 数据**：CI 把 Loyalsoldier 的 GeoSite/GeoIP 转成 sing-box 的 SRS 格式，随安装包附带一份，离线也能启动。sing-box 每 24 小时会去 GitHub Pages 检查一次更新。

## 不做什么

- 不在安装包里锁定 sing-box 版本。应用配置时，Steer 会让实际安装的 sing-box 检查生成的配置并读取它的编译选项，不兼容就直接报错，让你换一个合适的版本。
- 引导 DNS（Bootstrap DNS）必须填 IP 地址，且固定直连。远程订阅和 Geo 数据里不能包含会在本机执行的操作。
- 启用时，配置里必须恰好有一条直连路由和一条默认规则。
- 三个测试网址都必须填写，且必须是 HTTPS，没有默认值。
- 没有自动故障切换、配置历史、自动回滚、手动回滚命令，也不记录每个连接实际走了哪个出口。
- 配置有错、sing-box 不支持或原生检查不通过，都会直接拒绝。已经切换到新配置后才出错的，保持出错时的状态，不会假装成功，也不会自动恢复旧配置。

## 文档

- [项目范围](docs/SCOPE.md)
- [架构](docs/ARCHITECTURE.md)
- [配置与使用](docs/CONFIGURATION.md)
- [开发与验证](docs/DEVELOPMENT.md)
- [打包与发布](docs/PACKAGING.md)
- [测试说明](tests/README.md)
- [Linux](docs/LINUX.md)
- [macOS](docs/MACOS.md)
- [WireGuard（macOS）](docs/WIREGUARD.md)：导入配置、按 AllowedIPs 分流、让对端访问本机服务、Endpoint 地址变化后自动刷新。

## 命令行

```sh
steer version
steer validate
steer apply
steer health
steer status
steer probe --kind direct
steer subscription status
steer geo-catalog --kind geosite
steer cleanup
```

对外的命令就是上面这几个：`version validate apply health status probe subscription geo-catalog cleanup`。编译出来的中间结果和平台执行计划属于内部细节，命令行和 RPC 都不提供。

Linux 版的源码目录是 `cmd/steer-linux`，安装后命令名同样是 `steer`。它额外带一个网页管理界面：

```sh
sudoedit /etc/steer/web.json
steer web-token
steer web
steer subscription status
```

GitHub Release 里有 `steer-linux-x86_64.tar.zst` 和 `steer-linux-aarch64.tar.zst`，包里带了一份构建时校验过的 Geo 数据，但不包含 sing-box、geoview 或 DAT 数据库。本仓库不打 deb、rpm、Arch 等发行版的包；发行版维护者请从源码 tag 构建，并安装为 `/usr/bin/steer`。

macOS 只提供 Apple Silicon 版的 `steer-macos-arm64.dmg`，用 Xcode 27.0 和 macOS 27 SDK 构建。应用里打包了后台程序、官方 sing-box（固定 SHA 校验）、Geo 数据和安装器。目前只做了 ad-hoc 签名、没有公证，第一次打开时需要按 macOS 打开「未认证开发者」应用的步骤手动确认。DMG 和 `SHA256SUMS` 附有 GitHub 构件证明（attestation），但它不能代替 Developer ID 签名，也不能让 Gatekeeper 放行。

## OpenWrt 软件源

OpenWrt 25.12.5 x86_64 可以直接添加 GitHub Pages 上的软件源，索引由 CI 签名：

```sh
wget -O /etc/apk/keys/steer-apk.pem \
  https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/steer-apk.pem
echo 'https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/packages.adb' \
  >> /etc/apk/repositories.d/customfeeds.list
apk update
apk add steer luci-app-steer luci-i18n-steer-zh-cn
```

软件源里有 `steer`、`luci-app-steer`、简体中文翻译，以及一份 sing-box x86_64 APK。这份 sing-box 是 SagerNet 官方构件，校验 SHA 后用 Steer 的密钥重新签名。Geo 数据直接包含在 `steer` 包里。请不要用 `--allow-untrusted` 跳过索引签名检查。

## 许可证

GPL-3.0-or-later。sing-box、Geo 数据等第三方组件遵循各自的许可证。Steer 不修改也不重新编译 sing-box，只是在 OpenWrt 软件源里转发经过校验的官方构件，并重新签名。
