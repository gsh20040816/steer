# 开发与验证

共享核心定义配置的含义，OpenWrt、Linux、macOS 三个适配器负责各自平台的资源和发布。改代码时改在对应的那一层，并同步更新受影响的界面和文档。

新增或重构界面时，请同时遵守 [界面开发约定](UI_DEVELOPMENT.md)：LuCI、Linux 网页和 SwiftUI 各自保留平台原生的样子，但协议字段、能力、操作结果这些内容只能有一份共享定义。

## 目录

```text
go/internal/{intent,compiler,apply,generation}  配置格式、编译、应用流程、运行配置文件
go/internal/{subscription,probe,capability}     订阅、测速、sing-box 能力检查
go/internal/uispec                              三端界面共用的描述文件（构建时生成）
go/internal/platform/openwrt                    OpenWrt 适配器
go/internal/platform/linux                      Linux systemd 适配器
go/internal/platform/macos                      macOS 适配器
go/cmd/steer-openwrt                            OpenWrt 命令行
go/cmd/steer-linux                              Linux 命令行和网页界面
go/cmd/steer-macos                              macOS 后台程序
go/cmd/steer-geodata-build                      CI 里生成和校验 Geo 数据的工具
macos/SteerApp                                  macOS 图形界面
linux                                           Linux 发布包里的示例配置和 systemd unit
luci-app-steer                                  LuCI 界面、RPC、权限、翻译
steer                                           OpenWrt 软件包
.github/workflows/geodata.yml                   定时把 Loyalsoldier 数据转成 SRS 并发布
tests/node                                      LuCI 和 Linux 网页的回归测试
tests/integration                               在一次性 OpenWrt 虚拟机和 Linux 容器里跑的集成测试
```

Go 模块路径是 `github.com/gsh20040816/steer/go`。OpenWrt 软件包和命令都叫 `steer`。

## 本地检查

```sh
cd go
gofmt -w <改过的 Go 文件>
go test -race ./...
go vet ./...

cd ..
node tests/node/luci_view_test.js
node tests/node/steer_helper_test.js
node tests/node/linux_web_test.js
python3 tests/check-luci-i18n.py
python3 tests/check-release-consistency.py
python3 tests/check-ui-contract.py

cd macos
swift test --disable-sandbox --build-system native
cd ..
git diff --check
```

平时改完跑相关的测试就行；提交和发布前把上面全部跑一遍。

## 各个包管什么

- `intent`：只定义用户能配置的内容。新增字段必须在各个平台上都有意义，并且同时加进 JSON/UCI 的解析和校验。
- `compiler`：输出最终的 sing-box 配置，以及需要的 sing-box 能力和 Geo 规则集。不输出 nftables 规则或服务管理相关的内容。
- `apply`：按顺序调用各平台的准备、启用、健康检查、收尾和停用。
- `generation`：只负责 `intent.json` 和 `sing-box.json` 两个文件，平台自己的文件由适配器添加。
- `subscription.Store`：接口要保持精简，共享代码不能依赖 UCI 命令。
- `probe`：只负责测量和生成报告；测什么、怎么启动临时 sing-box、日志放哪，由各平台决定。
- `platform/openwrt`：UCI、nftables、策略路由、procd 和 OpenWrt 的目录只由它管理。
- `internal/geodata`：Geo 数据清单、分类查询和文件完整性校验。

## 对外接口

命令行只公开这些：

```text
version validate apply health status probe subscription geo-catalog cleanup
```

`_start` 只给 init 脚本用，不算公开接口。不要再公开 compile、plan、prepare、capabilities 或 rollback。RPC 只提供界面需要的操作。状态只包含 `healthy` 和 `last_apply`，配置是否合法由 validate 单独返回。

## Linux 发布约定

1. 源码目录叫 `cmd/steer-linux`，安装后固定为 `/usr/bin/steer`。
2. 普通提交的 CI 只跑测试，并编译一下 x86_64 和 aarch64 确认能编译通过；不打 tar.zst 包，也不打 deb、rpm、pkg.tar 等发行版包。
3. 安装包和发行版包都使用经过清单校验的 SRS 数据，不读用户指定的 DAT 文件，也没有第二份平台配置。
4. sing-box、nftables、iproute2 和 ca-certificates 由系统包管理器提供；geoview 只在 CI 生成 Geo 数据时用到。
5. 打 `v*` tag 时，OpenWrt、Linux、macOS 的正式产物都从这个 tag 的源码重新构建，不复用其他 CI 运行的产物。

macOS 用 launchd、utun 和管理员授权。SwiftUI 界面和 LuCI、Linux 网页地位相同，不要在里面重新实现规则、路由、DNS、订阅、校验或应用的逻辑。

界面代码可以有平台自己的布局和控件绑定，但不能自己维护一份协议支持表或完整的校验逻辑。生成的界面描述文件必须来自 `internal/uispec`，`check-ui-contract.py` 会检查三端是否一致。

## 真机验证和发布条件

`tests/integration/run-openwrt-vm.sh` 只能在一次性的 OpenWrt 25.12.5 x86/64 虚拟机上运行，它会覆盖 UCI、运行目录、nftables 和 procd。覆盖的内容包括：公开 RPC、通过真实 ucode RPC 导入单条/多行/Base64 节点、正常应用/重载/重启、DNS 转发规则、sing-box 1.14 原生 MAC 规则、停用后重新启用，以及非法配置被直接拒绝。

`tests/integration/run-linux-system.sh` 只能在设置了 `STEER_LINUX_SYSTEM_TEST=1` 的一次性特权 systemd 容器里运行。每次提交的 CI 都会用固定的 Debian 镜像、校验过 SHA-256 的 sing-box 1.14.1 musl 版和当前的 SRS 数据，在独立的网络命名空间里测试本机和转发流量的 IPv4/IPv6 TCP、UDP、DNS，监听端口的访问限制，停用/启用，服务重启和 nftables 重启后的恢复。这些 Linux 专属的检查只放在平台集成测试里，不放进共享核心的测试。

发布条件：

1. 本地检查全部通过。
2. 稳定版要求这个提交在 CI 里的 Ubuntu、Linux systemd、macOS arm64 任务全部通过。GitHub Actions 出故障时，预发布版可以用完整的本地检查代替。
3. tag 构建用官方 OpenWrt SDK 构建 Steer、LuCI 和中文翻译三个 APK，并校验、重签官方 sing-box APK；同时构建两个 Linux tar.zst 和一个 macOS arm64 DMG。
4. 发布包里的所有文件都来自要发布的这个提交。
5. 安装后运行 `validate`、`health`、`status` 并手动测一次。
6. 预发布版在真实设备上达到稳定版标准后，再发稳定版。
