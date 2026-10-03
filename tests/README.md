# 测试说明

测试分四层：共享核心、各平台适配器、三个界面、在目标系统上跑的集成测试。

## Go

```sh
cd go
go test -race ./...
go vet ./...
```

共享核心的测试覆盖：schema 9、JSON/UCI 的严格解析、引用和链式代理的校验、编译结果是否稳定、每条路由的独立出口、DNS 通道、Geo 规则集和数据清单、应用流程、运行配置文件、订阅合并、测速。

- **OpenWrt**：执行计划、nftables 规则、启用、健康检查和日志。
- **Linux**：本机和转发流量的执行计划、DNS 接管、本机目标地址的例外规则、监听端口保护、源 MAC 规则、JSON 原子写入和 ETag 冲突、systemd 和应用流程、网页界面的 token 认证和 CSP、开关失败后的回滚、临时测速进程的流量标记、静态编译。
- **macOS**：TUN 计划、端口 53 劫持、JSON 存储、运行配置、launchd 和平台限制。

## 界面和发布检查

```sh
node tests/node/luci_view_test.js
node tests/node/steer_helper_test.js
node tests/node/linux_web_test.js
python3 tests/check-luci-i18n.py
python3 tests/check-release-consistency.py
python3 tests/check-ui-contract.py
python3 tests/check-macos-packaging.py   # 只在 macOS 上有实际作用

cd macos
swift test --disable-sandbox --build-system native
```

各个文件测什么：

- `luci_view_test.js`：LuCI 表单、每个 RPC 的权限、本地代理的暴露和认证要求、前置代理可以清空、节点和路由的测试按钮、总览页的渲染和布局，以及 rpcd 里节点导入和导出的行为。
- `steer_helper_test.js`：LuCI 提交 UCI 后应用过程的提示、错误信息的本地化、单独的校验、会话权限。
- `linux_web_test.js`：Linux 网页的开关和冲突回滚、总览页、规则和节点的标签、SSH 私钥、本地代理认证、高级 JSON 编辑、无效 JSON 时阻止离开页面、放弃修改前的确认。
- Swift 测试：本地代理、订阅、测速、总览里 Saved/Active 的状态、提交前的检查。
- `check-luci-i18n.py`：LuCI 里出现的文字都有中文翻译。
- `check-release-consistency.py`：同一个版本号或校验值在不同文件里是否一致（sing-box 版本和 SHA、geoview 提交、两个 OpenWrt 包的版本、Arch 的 PKGBUILD 和 .SRCINFO），GitHub Actions 是否都固定到具体提交，LuCI 的 rpcd 脚本是否没有调用 shell。
- `check-ui-contract.py`：生成的界面描述文件是最新的，LuCI 菜单和它一致。
- `check-macos-packaging.py`：把安装器里清除 plist 隔离属性的那段脚本拿出来，在真实的扩展属性上运行一遍。

三端共用的测试数据放在 `ui/` 下：

- `subscription-status-fixtures.json`：订阅从未拉取、成功、有节点被跳过、成功后又失败、已停用、部分节点过期且被引用。
- `probe-diagnostics-fixtures.json`：测速结果的脱敏、界面上显示的摘要、端口 53 劫持检查、启用和停用时能做的操作。
- `state-lifecycle-fixtures.json`：全新安装、等待停用、应用失败、正常运行时的 Draft/Saved/Active 状态。
- `validation-issue-fixtures.json`：保存失败时问题的定位和不含凭据的错误信息。
- `collection-reference-fixtures.json`：删除节点、路由、DNS、本地代理、订阅时的引用保护。
- `rule-summary-fixtures.json`：规则匹配条件的摘要，以及 DNS 阶段和连接阶段的区别。
- `form-input-fixtures.json`：测试网址、订阅网址、时间间隔、DNS HTTP 路径的格式。
- `macos-system-component-fixtures.json`：macOS 各组件缺失或版本不一致的情况，区分「安装是否完整」和「是否正在运行」。

`internal/subscription` 的 Go 测试覆盖代理链接、多行和 Base64 订阅的解析，以及每种协议导出后再导入不丢信息。

## OpenWrt 虚拟机

`tests/integration/run-openwrt-vm.sh` 只能在一次性的 OpenWrt 25.12.5 x86/64 虚拟机上运行，事先要准备好匹配版本的 sing-box、完整的 SRS 数据和要测的 Steer。脚本会改写 `/etc/config/steer`、`/run/steer`、nftables、策略路由和 procd，**不要在正在使用的路由器上运行**。

它测试：公开的 RPC、Geo 分类查询、通过真实 ucode RPC 导入单条/多行/Base64 节点、典型配置的校验、正常应用、LuCI 提交 UCI 后自动应用、health/status、DNS 转发规则和 sing-box 1.14 原生 MAC 规则、fw4 重载、服务重启和重载、非法字段被直接拒绝、停用后重新启用。编译结果的细节由 Go 测试覆盖，不为了测试再公开 compile/plan/prepare 命令。

## Linux systemd 容器

`tests/integration/run-linux-system.sh` 只能在设置了 `STEER_LINUX_SYSTEM_TEST=1` 的一次性特权 systemd 容器里运行，不要在正常使用的机器上运行。CI 用 `tests/integration/linux-system.Dockerfile` 构建固定的 Debian 环境，挂载本次构建的 Steer、同一次校验过的 SRS 数据，以及校验过 SHA-256 的 sing-box 1.14.1 musl 版。

容器启动本身偶尔会失败，所以 CI 只对这一步重试：镜像只构建一次，容器最多重建 3 次，每次最多等 30 秒，直到 systemd 能响应且 `systemd-resolved` 已运行。每次失败都会打印容器状态、失败的 unit 和本次启动的日志。真正的 `run-linux-system.sh` 只跑一次，失败不重试。

脚本会建立 upstream 和 client 两个网络命名空间，固定 client 的 MAC 地址，先确认网络隔离，再把默认路由切到没有公网出口的 upstream。启用的配置里用到了 `geosite:cn` 和源 MAC 规则，所以即使 GitHub Pages 访问不了，服务也必须能靠安装包里的 Geo 数据启动。

测试内容：

- 本机和转发流量的 IPv4/IPv6 TCP、UDP，以及 UDP/TCP 53 端口的 DNS。
- DNS 请求发往一个不存在的目标地址，只有经过 Steer 的转发和独立的 DNS 上游才会成功。同时检查本机目标地址的 nftables DNS 计数器在增长，并确认 `steer0` 已在 systemd-resolved 里注册了 DNS。
- 1053/1054 端口不能被局域网当成 DNS 服务器直接访问。
- 服务重启、`nftables.service` 重启、停用后重新启用。

## 发布前完整检查

```sh
cd go && go test -race ./... && go vet ./...
cd ..
node tests/node/luci_view_test.js
node tests/node/steer_helper_test.js
node tests/node/linux_web_test.js
python3 tests/check-luci-i18n.py
python3 tests/check-release-consistency.py
python3 tests/check-ui-contract.py
sh -n tests/integration/run-openwrt-vm.sh
sh -n tests/integration/run-linux-system.sh
(cd macos && swift test --disable-sandbox --build-system native)
git diff --check
```

每个分支提交和 PR 都会在 CI 里跑：Ubuntu 上的 Linux/OpenWrt 测试、Linux systemd 容器集成测试、arm64 macOS 上的 Go 和 Swift 测试（Xcode 27.0、macOS 27 SDK）。CI 不限制并发，也不保存正式发布包。

打 tag 的规则：

- tag 指向的提交必须已经在 master 上。
- 稳定版还要求同一个提交在 master 上的 CI 已经通过。GitHub Actions 出故障时，预发布版可以用上面的完整本地检查代替。
- 如果 tag 推送事件丢了，可以手动触发同一个 tag 的发布流程；指定分支名会被拒绝。
- OpenWrt SDK 构建、Linux 打包、macOS DMG、构件证明和发布，全部在同一次 tag 构建里完成。

## 单独验证 DNS 接管

先导出当前 Linux/OpenWrt 计划生成的测试配置和防火墙规则（只用内置的 hosts 数据，不访问公网）：

```sh
cd go
STEER_DNS_FIXTURE_DIR=/tmp/steer-dns-fixtures go test ./internal/platform -run TestExportNativeDNSFixtures
```

把对应平台的 `.json`、`.nft` 和 `tests/integration/check-native-dns.py` 复制到测试机上运行：

```sh
# Linux，用户命名空间，不需要 sudo
unshare -Ur python3 check-native-dns.py linux.json linux.nft
# OpenWrt，root
python3 check-native-dns.py openwrt.json openwrt.nft
```

脚本会先创建独立的网络和挂载命名空间，并屏蔽宿主的 D-Bus，不会改动宿主的路由、DNS 或正在运行的 Steer。测试内容：本机和转发流量的 IPv4/IPv6 UDP/TCP 53；本机目标地址的兼容规则（包括发往 127.0.0.1、127.0.0.53、::1 和本机自己 IPv4/IPv6 地址的查询）；同一个 UDP 源端口先发 DNS 再发 STUN 的回归情况。需要 Python 3、ip-full、nft、dig、mount，以及支持命名空间和 TUN 的内核。

macOS 的 DNS 接管测试覆盖：自动/手动 DNS 的恢复、网络服务改名、新增服务、用户之后自己修改、部分写入失败、恢复失败后重试、sing-box 退出或被取消。运行 `STEER_TEST_SYSTEM_DNS_READ=1 go test ./internal/platform/macos -run TestReadSystemDNSPreferences -v` 可以只读地检查本机网络服务的发现结果，不会修改系统 DNS。

## OpenWrt netlink 故障恢复

`check-netlink-guard.py <steer-openwrt> <sing-box>` 在独立的网络和挂载命名空间里运行：暂停测试用的 sing-box，制造一批积压的路由通知，检查 CPU 持续占满后能否自动恢复、十分钟的冷却期，以及停止时子进程是否退出。它不使用正式配置。

在非初始命名空间里 `rmem_default` 通常是只读的，这时仍会测试降级后的恢复；如果要测试接收缓冲区的调大，需要在可写的环境里通过 netlink diag 检查实际的 socket 缓冲区大小，以及系统默认值是否恢复。CI 会和 DNS 集成测试一起跑这个测试。

这个测试按进程实际拥有的路由通知订阅来检查，不假设 sing-box 内部有几个 socket。sing-box 1.14.1 已经修复了接收溢出时卡死的问题，能正常处理突发、不被误重启就算通过；还有旧问题的版本则继续验证自动恢复和冷却期。

## IPv6 内核直连

```sh
cd go
STEER_BYPASS_FIXTURE_DIR=/tmp/steer-bypass-fixtures go test ./internal/platform -run TestExportDirectBypassFixtures
cd ..
unshare -Ur python3 tests/integration/check-direct-bypass.py /tmp/steer-bypass-fixtures linux
```

第一步用共享编译器和两个平台的真实配置导出 off/static/dns 三种模式的测试配置。第二步在 Linux 上运行（root 可以省略 `unshare -Ur`），把最后一个参数换成 `openwrt` 就是测 OpenWrt。

脚本自己隔离网络和挂载命名空间，搭建一个没有公网出口的 client/router/server 拓扑，测试：TCP/UDP 的远端 IPv6 源地址、源 MAC、DNS 映射、排在前面的代理/拒绝/协议规则能否挡住直连、嗅探后才确定的直连，以及显式 HTTP 代理。需要 sing-box、ip、nft、dig、Python 3，以及允许用户/网络/挂载命名空间的内核。CI 用校验过的官方 sing-box 1.14.1 对两个平台都跑一遍。

## WireGuard

把 `STEER_WG_SING_BOX` 设成校验过的 sing-box 1.14.1 路径，在 `go` 目录运行：

```sh
go test ./internal/wireguard -run TestNativeWireGuardRemoteAccess -count=1 -v
```

这个测试只用用户态的 WireGuard 端点和回环端口，不修改系统路由。macOS 发布流程会自动运行它。

## ICMP

在 `go` 目录把 `STEER_ICMP_FIXTURE_DIR` 设成一个绝对路径，运行 `go test ./internal/platform -run TestExportICMPFixtures`。然后在有网络命名空间权限的一次性 Linux 容器或 OpenWrt 上运行 `python3 tests/integration/check-icmp.py <fixtures> [linux openwrt macos]`。脚本先隔离网络和挂载命名空间再建测试链路，不修改宿主路由。

测试内容：IPv4/IPv6 真实的 ping 回应；目标主动丢包时不会伪造回应；Linux/OpenWrt 上本机和转发流量、TUN 源地址、TTL/Hop Limit 超时。每种情况都分别在有和没有（未使用的）WireGuard 端点时跑一遍，确保开关隧道不影响普通 ping。macOS 这里只是在 Linux 上验证配置的行为，真实 Mac 上的表现和 WireGuard 对端的连通性需要另外测。
