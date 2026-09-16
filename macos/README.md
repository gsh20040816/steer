# macOS targets

macOS 的正式产品由 SwiftUI GUI 前端和 LaunchDaemon 后端组成：

```text
SteerApp
  ├── read config / validate / status（无授权弹窗）
  ├── 首次安装 embedded payload（一次管理员授权）
  └── save / apply / probe / subscription update-clean（后续免密）
             ↓ /var/run/steer/control.sock
steer-macos _control（root、仅允许固定配置、探测与订阅操作）
             ↓
steer-macos helper
  └── launchd lifecycle → external sing-box → Darwin TUN
```

GUI 与 OpenWrt LuCI、Linux Web 同级：它编辑同一份 Canonical Intent，并调用平台后端完成校验、保存、Apply 和状态读取。GUI 不包含代理数据面，也不复制 Go 核心语义。

日常配置使用原生字段编辑器：基础设置覆盖 Main、探测、DNS 缓存与 Bootstrap；节点、路由、DNS Profile、本地代理、规则和订阅分别使用原生 Form。内部 Canonical ID 由 GUI 管理，不出现在普通列表或弹窗；完整 JSON 仅保留为侧栏“高级”区域的兜底入口。所有页面和菜单栏共用明确分离的 Save、Apply Saved、Save and Apply；Enable 使用最新 Saved 配置独立启停，保留未保存或格式错误的 Draft。

当前源码入口：

- `SteerApp/`：SwiftUI 配置与运维前端；
- `../go/internal/platform/macos/`：Darwin TUN、DNS port-53 capture、generation 和 launchd backend；
- `launchd/`：运行、常驻 root control 与订阅调度 LaunchDaemon plist；
- `scripts/build-app-bundle.sh`：唯一的 App/DMG 组装、验收和 ad-hoc 签名脚本；
- `scripts/install-embedded-payload.sh`：正式 App 内置的固定 payload 安装/Repair 工具；
- `scripts/uninstall-embedded-payload.sh`：正式 App 内置的固定路径、幂等卸载器，默认保留 config/state/logs；
- `scripts/install-launchdaemon.sh`：源码开发时构建 helper、发现 sing-box、安装并 bootstrap 服务；
- `../go/cmd/steer-macos/`：macOS 后端 CLI。

## 正式 DMG

tag workflow 在 GitHub `xcode-27` Apple Silicon runner 上使用 Xcode 27.0/macOS 27 SDK 生成：

```text
steer-macos-arm64.dmg
```

将 App 拖入 `/Applications` 后，首次在“系统”页安装系统组件并输入一次管理员密码。之后 GUI Save/Apply 与订阅更新/清理经 `root:admin 0660` socket 和 Darwin peer credentials 保护的结构化 IPC 完成，不执行任意 shell 命令，也不再请求密码。

系统页逐项验收 helper、sing-box、三个 LaunchDaemon plist/注册状态、config、Geo seed 与 control socket。任何缺失或版本不一致都会显示 Repair；卸载默认保留 config/state/logs，删除用户数据必须再经过一次独立确认。

macOS 27 会拒绝加载带有 `com.apple.quarantine` 下载隔离标记的 LaunchDaemon plist，并报告 `155: Refusing to execute/trust quarantined program/file`。安装器在校验 App 签名与 payload 校验和后，仅清除三个已安装 Steer plist 上的此标记，再注册服务；不会递归清理 App、可执行文件或其他扩展属性。旧安装器会在 Repair 时重新复制该标记，需使用包含此修复的 App 再执行“修复系统组件”。

当前只有 ad-hoc 签名，没有 Developer ID 和 notarization。用户仍需按 macOS 未认证开发者流程首次确认；GitHub artifact attestation 可验证来源，但不会让 Gatekeeper 自动放行。

## 源码安装和运行

先安装外部 sing-box：

```sh
brew install sing-box
```

再安装后端：

```sh
sudo macos/scripts/install-launchdaemon.sh
sudo /usr/local/libexec/steer/steer-macos validate
sudo /usr/local/libexec/steer/steer-macos apply
sudo /usr/local/libexec/steer/steer-macos health
```

构建并启动 GUI：

```sh
cd macos
swift build --disable-sandbox
swift run SteerApp
```

安装器把配置设为 `root:admin 0640`，运行配置继续保持 root 私有；GUI 启动及状态刷新通过受限 control socket 获取权威 Saved/Active 摘要，读取失败会明确显示异常，Validate、探测和 Geo catalog 也不会弹出管理员授权。概览探测经受限 control socket 从 Saved 配置读取目标，并直接使用 Mac 当前网络环境；没有 Active 时仍可运行。报告绑定 Saved digest、测试时可用的 Active identity 和 tested_at，Saved 或网络环境改变后旧结果显示为过期。开发安装脚本会根据 `command -v sing-box` 自动处理 Apple Silicon 与 Intel Homebrew 前缀，把选中的构件复制为 root-owned `/usr/local/libexec/steer/sing-box`，并安装 control 与订阅调度服务；后续 Save/Apply、探测与订阅更新/清理和正式 App 使用同一受限免密 IPC。

GUI Load 会保存当前 Saved revision，后续 Save/Apply 通过 `expected_revision` 做乐观并发保护。订阅 timer 或手动更新先改变 Saved 节点库存时，旧 Draft 不能覆盖它；GUI 会让用户选择 Reload Saved、保留本地 Draft 或显式覆盖。手动更新期间的新编辑不会被完成后的 reload 清除，订阅库存更新也不会自动 Apply。

每个 App 生命周期只初始化一次 Draft，关闭主窗口再从菜单栏打开不会重新读取磁盘。Reload、安装/Repair 和退出在 dirty 时统一要求 Save、Discard 或 Cancel；Cancel 不改变 Draft、Saved 或 Active。首次安装后不会无条件覆盖安装前的编辑：Save 会在组件可用后保存并保留该 Draft，只有显式 Discard 才重新载入安装后的 Saved 配置。Apply Saved 始终读取并应用磁盘 Saved，不会夹带独立的本地 Draft；Apply 失败会明确报告，但 Active 仍只来自后端返回的实际 generation。当前 macOS 后端未提供可靠的 runtime-projection pending fact，因此 GUI 不根据任意 Save 猜测“待 Apply”，Apply Saved 始终保持为显式可用动作。

## TUN、DNS 和 Geo

sing-box 负责 Darwin utun 和 `auto_route`；macOS plan 不设置 `auto_redirect`、nftables 或 pf，也不修改系统 DNS。平台层静态接管 IPv4 RFC1918、CGNAT 与 IPv6 ULA。route 顺序固定为：`steer-tun + TCP/UDP + 目标端口 53 -> hijack-dns`、上述私网 `-> Direct`、sniff、用户公网规则。回环、链路本地、组播和文档/保留地址仍排除；普通 global IPv6 on-link 地址按公网规则处理，DoH/DoT 不属于 Do53 劫持。该 plan 不依赖接口或 DHCP 状态，网络变化不会隐式 Apply Saved config。

Geo 不是 macOS 语义限制。正式 DMG 已内置并校验 tag workflow 使用的精确 `geodata-seed/`；源码开发需把相同 seed 放入 `/Library/Application Support/Steer/geodata-seed/`。Apply 会按 manifest 校验所需 SRS；目标机不安装 geoview，也不读取 DAT。

从旧版升级到 0.9.13 后，在“系统”页更新/修复系统组件，使常驻 control 服务与新客户端同步。新启停命令只修改最新 Saved 的 enabled，订阅自动更新不会导致开关弹出整份配置覆盖选择；普通 Save/Save and Apply 继续执行版本冲突保护。
