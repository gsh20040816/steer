# 开发与验证

共享核心负责配置语义，OpenWrt、Linux 与 macOS 适配器负责平台资源和发布。修改应落在相应责任层，并同步受影响的前端和文档。

三端前端的新增与重构还必须遵守 [原生前端与共享控制面开发约束](UI_DEVELOPMENT.md)：保留 LuCI、Linux Web 和 SwiftUI 原生呈现，但协议字段、能力、操作结果和控制语义只能有一份共享规格。

## 仓库结构

```text
go/internal/{intent,compiler,apply,generation}  共享语义和生命周期
go/internal/{subscription,probe,capability}     共享服务
go/internal/uispec                              三端前端构建期共享规格
go/internal/platform/openwrt                    OpenWrt 适配器
go/internal/platform/linux                      Linux systemd 主机/转发流量适配器
go/internal/platform/macos                      macOS launchd/Darwin TUN 适配器
go/cmd/steer-openwrt                            OpenWrt CLI 源码 target
go/cmd/steer-linux                              Linux CLI/Web
go/cmd/steer-macos                              macOS helper/LaunchDaemon 入口
macos/SteerApp                                  macOS SwiftUI 前端
go/cmd/steer-geodata-build                      CI SRS 生成与完整 seed 校验工具
linux                                           Linux 通用发行资产与 systemd unit
luci-app-steer                                  LuCI、RPC、ACL、翻译
steer                                           OpenWrt 控制器包
.github/workflows/geodata.yml                    定时发布当前 Loyalsoldier-compatible SRS
tests/node                                      LuCI/分享 URL 回归
tests/integration                               一次性 OpenWrt VM 与 Linux systemd 容器正常路径
```

共享模块路径为 `github.com/gsh20040816/steer/go`，OpenWrt 控制器包和命令源码均使用统一名称 `steer`。

## 本地验证

```sh
cd go
gofmt -w <changed-go-files>
go test -race ./...
go vet ./...

cd ..
node tests/node/luci_view_test.js
node tests/node/steer_helper_test.js
node tests/node/linux_web_test.js
python3 tests/check-luci-i18n.py
python3 tests/check-package-boundaries.py
python3 tests/check-build-cache.py
python3 tests/check-linux-packaging.py
python3 tests/check-ui-contract.py

cd macos
swift test --disable-sandbox
cd ..
git diff --check
```

修改后运行相关测试；提交和发布前运行完整集合。测试验证配置、Apply、启动、订阅和诊断的实际行为，以及错误输入和失败操作的处理。生成文件检查验证输出一致性。

## 包职责

- `intent` 只定义用户语义。新增字段必须有跨平台意义，并同时进入严格 JSON/UCI codec 和校验。
- `compiler` 输出最终 sing-box 配置与能力/Geo 需求，不输出 nftables、服务管理器或公共平台计划。
- `apply` 同步编排 Backend 的准备、激活、健康检查、收尾和禁用操作。
- `generation` 只拥有 `intent.json` 和 `sing-box.json`；平台文件由平台适配器添加。
- `subscription.Store` 必须保持窄接口，不能让共享逻辑依赖 UCI 命令。
- `probe` 只测量和报告；目标选择、临时核心进程与日志路径由平台适配器处理。
- `platform/openwrt` 是 UCI、nftables、策略路由、procd 和 OpenWrt 目录的唯一所有者；`internal/geodata` 共享 seed manifest、selector 与文件完整性校验。

## 公共契约

公开 CLI 只有：

```text
version validate apply health status probe subscription geo-catalog cleanup
```

`_start` 仅供 init 脚本使用，不属于公共接口。不得重新公开 compile、plan、prepare、capabilities 或 rollback。RPC 只包装用户/界面需要的公共操作；状态对象固定为 `healthy + last_apply`，合法性由 validate 单独返回。

## Linux 交付边界

Linux 适配器与上游发行资产已经建立，后续修改必须保持：

1. 源码 target 继续叫 `cmd/steer-linux`，安装名固定为 `/usr/bin/steer`；
2. 每个分支 commit 的 CI 只运行测试和 x86_64/aarch64 编译 smoke，不归档 tar.zst，也不构建 deb、rpm、pkg.tar 等发行版包；
3. Linux 归档和发行版包安装经 manifest 完整校验的 SRS seed，不读取用户指定的 DAT 或第二份 platform settings；
4. sing-box、nftables、iproute2 和 ca-certificates 由系统包管理器提供，geoview 只存在于 CI 生成器的构建依赖中；
5. `v*` tag workflow 从 tag 源码构建 OpenWrt、Linux 与 macOS 正式产物，不复用其他 run 的构件。

macOS 使用 launchd、Darwin utun 和管理员授权；SwiftUI GUI 与 LuCI、Linux Web 同为平台前端。GUI 不得复制共享规则、路由、DNS、订阅、Validate 或 Apply 语义。

平台 UI 文件可以包含原生布局和控件绑定，不得包含独立维护的协议矩阵或完整校验 switch。构建生成文件必须来自 `internal/uispec`，并由 UI contract 检查保证三端一致。

## 目标系统验收与发布门

`tests/integration/run-openwrt-vm.sh` 只能运行在一次性 OpenWrt 25.12.5 x86/64 VM。它会替换 UCI、运行目录、nftables 和 procd，并覆盖公开 RPC、通过目标 ucode RPC 的单条/多行/Base64 节点导入、正常 Apply/reload/restart、DNS shim、1.14 原生 MAC 规则、禁用/重新启用及非法配置 fail-fast。

`tests/integration/run-linux-system.sh` 只能运行在显式设置 `STEER_LINUX_SYSTEM_TEST=1` 的一次性 privileged systemd 容器。每个 commit 的 CI 使用固定 Debian 基础镜像、校验过 SHA-256 的 sing-box 1.14.0 musl 产物和经过验证的当前 SRS seed，在独立 netns 中覆盖主机与转发流量的 IPv4/IPv6 TCP、UDP、UDP/TCP DNS、listener 访问限制、禁用/启用、服务重启和 nftables 重启恢复。Linux 的 systemd、nftables、端口与 netns 断言只属于平台验收，不进入共享核心测试。

发布门：

1. 全部本地检查通过；
2. 稳定版要求 commit CI 的 Ubuntu、Linux systemd、macOS arm64 与 macOS x86_64 jobs 全部通过；Actions 服务降级时，预发布可由完整本地发布门替代这项独立 CI 前置条件；
3. tag workflow 使用官方 OpenWrt SDK 构建 Steer/LuCI 三个 APK，并校验、重签官方 sing-box APK；Linux 并行构建两个通用 tar.zst；macOS 原生构建两个 DMG；
4. release bundle 中全部产物来自拟发布 commit；
5. 安装后运行 `validate`、`health`、`status` 和显式测试；
6. 预发布版本在真实目标上满足稳定门槛后再晋级稳定版。
