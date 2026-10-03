# macOS

macOS 版由 SwiftUI 图形界面和 LaunchDaemon 后台服务组成：

```text
SteerApp
  ├── 读取配置、校验、查看状态（不需要密码）
  ├── 第一次安装系统组件（输入一次管理员密码）
  └── 保存、应用、测试、订阅更新和清理（之后不再需要密码）
             ↓ /var/run/steer/control.sock
steer-macos _control（root 运行，只接受固定的几类操作）
             ↓
steer-macos
  └── launchd → sing-box → Darwin TUN
```

图形界面和 LuCI、Linux 网页界面是同一层的东西：编辑同一种配置，通过后台完成校验、保存、Apply 和状态查询。它不处理流量，也不重复实现 Go 核心的逻辑。用户层面的说明见 [docs/MACOS.md](../docs/MACOS.md)。

## 目录

- `SteerApp/`：SwiftUI 界面；
- `SteerAppTests/`：Swift 测试；
- `launchd/`：三个 LaunchDaemon 的 plist，分别负责运行 sing-box、常驻 control 服务和订阅定时更新；
- `scripts/build-app-bundle.sh`：组装 App 和 DMG、做检查、ad-hoc 签名，只有这一个打包脚本；
- `scripts/install-embedded-payload.sh`：App 内置的系统组件安装/修复脚本；
- `scripts/uninstall-embedded-payload.sh`：App 内置的卸载脚本，可重复执行，默认保留配置、状态和日志；
- `scripts/install-launchdaemon.sh`：源码开发用，构建后台程序、找到 sing-box、安装并启动服务；
- `../go/cmd/steer-macos/`：后台命令行；
- `../go/internal/platform/macos/`：TUN、DNS、配置目录和 launchd 相关的实现。

## 正式 DMG

打 tag 后，release workflow 在 GitHub 的 `xcode-27` Apple Silicon 机器上用 Xcode 27.0 和 macOS 27 SDK 构建 `steer-macos-arm64.dmg`。

- 把 App 拖进 `/Applications`，第一次在「系统」页安装系统组件，输入一次管理员密码。之后的保存、应用、订阅更新和清理都通过 control socket 完成，不再要密码。socket 权限是 `root:admin 0660`，后台还会用 peer credential 确认请求方属于管理员组；请求是结构化的，不能执行任意命令。
- 「系统」页逐项检查后台程序、sing-box、三个 LaunchDaemon（文件和注册状态）、配置、Geo 数据和 control socket。缺了或者版本对不上就显示「修复」。卸载默认保留配置、状态和日志，要删除用户数据还得再确认一次。
- macOS 27 拒绝加载带 `com.apple.quarantine` 标记的 LaunchDaemon plist，报错 `155: Refusing to execute/trust quarantined program/file`。安装脚本在校验 App 签名和文件校验和之后，只去掉三个已安装 plist 上的这个标记，然后再注册服务，不会递归清理 App 或其他文件。
- 目前只有 ad-hoc 签名，没有 Developer ID 和公证。第一次打开要按「未认证开发者」的流程手动确认。GitHub 的构件证明可以验证来源，但不能让 Gatekeeper 放行。

## 从源码安装

先装 sing-box：

```sh
brew install sing-box
```

再装后台服务：

```sh
sudo macos/scripts/install-launchdaemon.sh
sudo /usr/local/libexec/steer/steer-macos validate
sudo /usr/local/libexec/steer/steer-macos apply
sudo /usr/local/libexec/steer/steer-macos health
```

安装脚本用 `command -v sing-box` 找到 sing-box（Apple Silicon 和 Intel 的 Homebrew 路径都行），复制成 root 所有的 `/usr/local/libexec/steer/sing-box`，并装好 control 和订阅定时服务。配置文件权限是 `root:admin 0640`，运行时配置只有 root 能读。之后的使用方式和正式 App 一样，都走 control socket。

源码开发需要自己把 Geo 数据放到 `/Library/Application Support/Steer/geodata-seed/`，正式 DMG 已经带了。目标机器不需要 geoview，也不读 DAT 文件。

构建并启动界面：

```sh
cd macos
swift build --disable-sandbox --build-system native
swift run --build-system native SteerApp
```

## 界面行为

- 基础设置编辑全局选项、测试网址、DNS 缓存和引导 DNS；节点、路由、DNS、本地代理、规则和订阅各有自己的表单。内部 ID 由界面自动生成，列表和弹窗里不显示。完整 JSON 编辑只在侧栏的「高级」里。
- 所有页面和菜单栏共用「保存」「应用已保存配置」「保存并应用」。「应用已保存配置」只应用磁盘上的配置，不会带上未保存的修改。后台没法可靠地判断已保存配置和正在运行的是否一致，所以这个按钮一直可点。
- 工具栏的启用开关只改已保存配置里的 `enabled` 再应用，未保存的修改（哪怕格式有错）原样保留。
- 打开 App 时读取一次配置，同时记下版本号；保存和应用时带上这个版本号。如果订阅定时更新或其他地方先改了配置，界面会让你选：重新载入、保留本地修改，或者强制覆盖。手动更新订阅期间如果你又改了东西，更新完成后不会自动重新载入。订阅更新从不自动 Apply。
- 关掉主窗口再从菜单栏打开，不会重新读配置。重新载入、安装/修复、退出这几个操作，在有未保存修改时都会问「保存 / 放弃 / 取消」。安装系统组件之后不会覆盖你之前的修改。
- 网络测试通过 control socket 读取已保存配置里的网址，直接用当前网络访问，Steer 没启用也能测。已保存配置或网络环境变化后，旧结果标为过期。状态读取失败会明确显示出错，不会当成服务已停止。

## TUN、DNS 和 Geo

- sing-box 管理 utun 和 `auto_route`。不用 `auto_redirect`、nftables 或 pf。
- TUN 的 `route_address` 只有默认路由，不额外添加私网路由。私网目标如果进了 TUN，会被规则送去直连。
- 规则顺序：TUN 里目标端口 53 的 TCP/UDP → `hijack-dns`，然后是 ICMP，然后私网 → 直连，然后嗅探和用户规则。回环、链路本地、组播、文档地址和其他保留地址不进 TUN。普通的公网 IPv6 地址（即使在同一链路上）按用户规则处理。DoH/DoT 不在 53 端口劫持范围内。
- sing-box 不会自己改系统 DNS，所以 `_run` 在 DNS 可用后把物理网络服务的 DNS 改成 Steer 的地址，退出时改回去。细节见 [ADR 0002](../docs/adr/0002-macos-launchd-tun.md)。
- 网络变化不会触发重新 Apply。

## WireGuard

0.11.1 起支持 WireGuard 隧道：导入 `.conf`、按 AllowedIPs 自动生成规则、让对端访问本机的 TCP/UDP 服务。Endpoint 地址变化时会重载核心，现有连接可能会断。详见 [WireGuard](../docs/WIREGUARD.md)。
