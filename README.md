# Steer

Steer 是一套严格、可解释的透明代理控制面。用户配置节点、逻辑路由、DNS Profile 和有序规则；共享 Go 核心负责 Canonical Intent、校验、编译、Apply 编排、订阅与测试，平台适配器负责网络资源和服务生命周期。

当前稳定版本为 **0.10.0**，公开配置为 **schema 9**。OpenWrt 25.12.5 x86/64 由主仓库提供 APK；Linux systemd 适配器提供 x86_64/aarch64 通用上游 tar.zst；macOS 提供 arm64/x86_64 原生 DMG、SwiftUI GUI、一次授权安装的 root control daemon 和 sing-box TUN 后端，无需 Apple Developer Program。

## 已实现能力

- 严格 first-match 规则，支持域名、GeoSite、目标 IP、GeoIP、源 CIDR、源 MAC、网络、协议、端口和本地代理入口。
- Direct、Reject、单节点逻辑路由；单节点路由可选择另一条单节点路由作为前置代理，任意深度但不得成环。为兼容现有配置，Reject 的 Canonical `kind` 仍为 `block`，但底层只生成 sing-box `reject` action。
- SOCKS、HTTP、Shadowsocks、VMess、VLESS、Trojan、Hysteria、ShadowTLS、TUIC、Hysteria2、AnyTLS、SSH、NaiveProxy 和本机 Tor 节点。
- UDP、TCP、DoT、DoH、DoQ、DoH3 DNS Profile；每个实际使用的 `(DNS Profile, Route)` 拥有独立传输路径。
- HTTP(S) 节点订阅、稳定节点 ID、无引用过期节点自动清理与引用保护。
- 三个可配置目标的当前网络环境测试，以及裸节点和完整路由链测试。
- OpenWrt UCI/LuCI、procd、sing-box TUN `auto_route`/`auto_redirect`、1.14 原生源 MAC 规则和最小 DNS nftables shim。
- Linux systemd 适配器源码：Canonical JSON、sing-box TUN `auto_route`/`strict_route`/`auto_redirect`、主机与 VM/Docker DNS 的双栈 nft shim、受保护的 wildcard DNS listener、nftables 重启联动、systemd 服务和 loopback Web API/UI。
- macOS SwiftUI GUI 与 LuCI、Linux Web 同为平台前端，直接编辑 Canonical Intent；首次安装系统组件需要一次管理员授权，之后 Save/Apply 通过 peer-credential 保护的受限 root IPC 免密完成，数据面由独立 LaunchDaemon 和 sing-box TUN 承担。
- LuCI、Linux Web 和 macOS SwiftUI 保留平台原生交互，同时由生成的共享 UI 规格统一协议字段、枚举、能力声明和“状态 / 配置 / 服务 / 高级”信息层级。
- CI 将 Loyalsoldier GeoSite/GeoIP 转换为完整 SRS seed；设备以 seed 离线启动，并由 sing-box 每 24 小时后台检查 Pages `latest`。

## 明确边界

- 不把 sing-box 版本写死在发行包依赖中；Apply 通过实际二进制的 native config check 和 build tags 判断能力，不满足时明确要求指定兼容版本/构建。
- Bootstrap DNS 必须是 IP 字面量并固定直连。远程订阅和 Geo 数据不能携带本地动作。
- 启用配置中必须恰好有一个 Direct 路由和一个 Default 规则。
- 三个测试 URL 都是必填 HTTPS scalar option，没有隐藏默认值。
- 没有自动故障转移、配置历史、自动回滚、人工 rollback 命令或运行时 outbound 命中追踪。
- 配置、能力或原生检查失败时直接拒绝；切换运行态后的失败保留现场，不伪装成功，也不自动恢复。

## 文档

- [范围与冻结规则](docs/SCOPE.md)
- [架构](docs/ARCHITECTURE.md)
- [配置与使用](docs/CONFIGURATION.md)
- [开发与验证](docs/DEVELOPMENT.md)
- [打包与发布](docs/PACKAGING.md)
- [测试说明](tests/README.md)
- [Linux 适配器](docs/LINUX.md)
- [macOS GUI 与 LaunchDaemon 适配器](docs/MACOS.md)

## 公共 CLI

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

公共命令固定为 `version validate apply health status probe subscription geo-catalog cleanup`。编译器中间结果和平台计划属于内部接口，不通过 CLI 或 RPC 暴露。

Linux 源码 target 是 `cmd/steer-linux`，安装后的产品命令统一为 `steer`，并提供 loopback Web 控制面：

```sh
sudoedit /etc/steer/web.json
steer web-token
steer web
steer subscription status
```

GitHub Release 提供 `steer-linux-x86_64.tar.zst` 和 `steer-linux-aarch64.tar.zst`，内含包构建时验证过的 SRS seed，不捆绑 sing-box、geoview 或 DAT 数据库。主仓库不构建 deb、rpm、Arch 等发行版包；发行版维护者应从 source tag 构建并安装为 `/usr/bin/steer`。

macOS Release 提供 `steer-macos-arm64.dmg` 和 `steer-macos-x86_64.dmg`。App 内置同架构 helper、经固定 SHA 校验的官方 sing-box、Geo seed 与首次安装器；当前采用 ad-hoc 签名且未公证，因此首次打开仍需按 macOS 未认证开发者流程手动确认。Release DMG 与 `SHA256SUMS` 提供 GitHub artifact attestation，但 attestation 不替代 Developer ID 或 Gatekeeper 放行。

## OpenWrt 软件源

OpenWrt 25.12.5 x86_64 可直接使用由 GitHub Pages 托管、CI 签名的稳定软件源：

```sh
wget -O /etc/apk/keys/steer-apk.pem \
  https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/steer-apk.pem
echo 'https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/packages.adb' \
  >> /etc/apk/repositories.d/customfeeds.list
apk update
apk add steer luci-app-steer luci-i18n-steer-zh-cn
```

软件源包含 `steer`、`luci-app-steer`、简体中文翻译以及经官方 SHA 校验后由 Steer key 重签的 SagerNet sing-box x86_64 APK。SRS seed 由 `steer` 包直接拥有；不得使用 `--allow-untrusted` 绕过索引签名。

## 许可证

GPL-3.0-or-later。sing-box、Geo 数据和其他第三方组件继续遵循各自许可证；Steer 不 fork 或重编译 sing-box，但 OpenWrt 软件源会镜像并重签经过摘要验证的官方构件。
