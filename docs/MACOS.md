# macOS 开发基线

macOS 由两个正式组成部分构成：SwiftUI GUI 是用户前端，root LaunchDaemon + 外部 sing-box 是运行后端。GUI 与 OpenWrt LuCI、Linux Web 处于同一层，只负责配置、操作和状态展示，不承载代理数据面。

```text
Steer GUI
  ├── 编辑 Canonical Intent
  ├── Read / Validate / Status（无授权弹窗）
  ├── 首次“安装系统组件”
  │          ↓ 一次 macOS 管理员授权
  └── Save / Apply / 订阅更新与清理（后续免密）
             ↓ /var/run/steer/control.sock
root LaunchDaemon: steer-macos _control
             ↓ 仅允许固定的配置与订阅操作
/usr/local/libexec/steer/steer-macos
             ├── launchctl bootstrap → root LaunchDaemon: steer-macos _run
             └── 每 15 分钟 → root LaunchDaemon: subscription update
root LaunchDaemon: steer-macos _run
             ↓ exec
sing-box run -c current/sing-box.json
             ↓
Darwin utun + auto_route
```

这条路径不要求 Apple Developer Program 或付费签名。sing-box 的 TUN inbound 官方支持 macOS；`auto_redirect` 是 Linux 路径，macOS 不使用它。[sing-box Tun](https://sing-box.sagernet.org/configuration/inbound/tun/)

## GUI 前端

`macos/SteerApp` 是 macOS 的正式配置与运维前端，不是代理运行时，也不维护第二份配置语义。它直接面向系统安装的 `steer-macos` helper：

- 总览：固定显示执行模型、Draft/Saved/Active 生命周期、当前 Draft 的六类配置规模与校验/聚合 Warning、最近 Apply 和快捷操作；私有 `_state` 从 Saved 编译投影与实际 Active 投影计算 pending Apply，普通摘要不显示内部 generation/digest 或原始错误链；
- 基础设置：用原生字段编辑 Main、探测 URL、DNS 缓存和 Bootstrap DNS；
- 节点、路由、DNS Profile、规则、订阅、本地代理：用原生 Table 与 Form 编辑同一份 draft collection，并支持拖动排序；普通界面只显示名称，不暴露内部 Canonical ID；
- Canonical JSON · 高级：只作为完整导入、排错和高级字段的兜底入口；
- 诊断：显示共享校验、最近 Apply、overview 测试操作及最新安全摘要、DNS 接管检查，以及按需加载的受限运行日志；Node/Route 最近测速结果显示在对应实体操作旁，不展示连续历史报告；
- 系统：逐项显示 helper、sing-box 版本/build tags、generation、last Apply、Geo seed version/rule count、三个 plist/LaunchDaemon、DNS capture boundary、配置与 control socket 的安装事实；缺失或版本不一致时可用固定 embedded payload Repair，并提供受控卸载。

所有页面的 toolbar 和菜单栏固定提供 Save、Apply Saved、Save and Apply 与全局 Enable，文案直接反映是否写入 Saved。Apply Saved 从磁盘读取当前 Saved 后再执行 revision-guarded Apply，即使本地 Draft dirty 也不会夹带或覆盖它；Apply 失败时 candidate 不会冒充 Active。切换 Enable 时把当前合法 Draft 连同新开关状态一起 Save and Apply，不因 dirty 而忽略其他修改；失败时分别显示 Saved 开关与后端真实 Active 状态。
全局 Enable 只在 Draft 语法无效、revision conflict、Draft guard 或写操作进行中时禁用，并通过原生 help 说明原因；Overview 不再维护第二个 Enable。合法 dirty Draft 切换成功后采用新的 Saved revision，若 Apply 部分失败，toolbar 开关仍表达已保存的期望状态，运行徽标继续表达真实 Active 状态。

读取系统配置、Status、Validate、探测和 Geo catalog 不弹出管理员授权。配置保持 `root:admin 0640`，不含密钥的 `current.json` generation 摘要可由 GUI 读取。概览探测通过同一个受限 Unix socket 交给 root daemon：请求只含固定的 kind/对象 ID/download 字段，不接受 URL、路径或命令；daemon 从 Saved 配置选择目标并直接使用 Mac 当前网络环境访问，因此没有 Active generation 时仍可测试。正式 App 首次安装内置系统组件时使用一次 macOS 标准管理员授权；之后 Save、Apply、探测和订阅更新/清理通过常驻 `com.steer.steer.control` root LaunchDaemon 完成，不再重复请求密码。

系统页将安装事实与运行激活事实分开显示。helper、sing-box、三个 plist、Canonical 配置、Geo seed、control/subscription LaunchDaemon 和 control socket 决定安装是否完整；`com.steer.steer` runtime LaunchDaemon 是否已加载只表示当前运行激活状态。Saved `main.enabled=false` 时 runtime LaunchDaemon 按设计未加载，系统组件仍为“已安装”，配置仍必须正常载入，也不得因此提示 Repair。

control daemon 只接受 schema 固定、大小受限的 `save`、`apply`、`probe`、只读 `diagnostics`、只读 `probe-results`、`subscription-update` 和 `subscription-clean` JSON 请求，不提供 shell、URL、路径或可执行文件参数。`probe` 和 `probe-results` 都只向 GUI 返回共享 `LatestProbeResult` 摘要；原始测量报告、Saved/Active identity 和完整错误链不跨入普通 SwiftUI。概览探测不切换运行态、不启动临时核心，也不依赖 LaunchDaemon/TUN 健康状态。socket 目录为 root-owned、不可由普通管理员替换；socket 本身为 `root:admin 0660`，服务端还使用 Darwin `LOCAL_PEERCRED` 再次校验 root/admin 调用者。候选配置仍经过共享严格解码与 canonical validation，写入使用 `root:admin 0640` 原子替换。GUI 不直接写 generation，不直接启动 sing-box，也不复制 Go 校验、编译、stale 或测速摘要逻辑。

GUI 每次 Load 同时保存配置内容的 SHA-256 revision；Save 与 Apply 必须携带该 `expected_revision`。control 在与订阅调度器共用的跨进程 operation lock 内先比较当前 Saved revision，再写入或切换运行态。不匹配时返回稳定的 `REVISION_CONFLICT`，Saved、Active 和本地 Draft 都不变。GUI 明确提供 Reload Saved、保留本地 Draft 和显式覆盖三种选择；显式覆盖仍使用冲突响应中的最新 revision 做第二次原子比较，不绕过并发保护。

订阅 timer 和手动 Update 只更新 Saved 节点库存，从不自动 Apply。手动更新开始后若 Draft 未变化，完成时可安全 reload；若用户在网络请求期间继续编辑，GUI 保留本地 Draft 并显示上述冲突选择，不能用更新结果静默替换编辑内容。订阅列表保留最近成功和最近失败两组事实；上游已移除且无 Route 引用的节点在更新时自动删除，仍被引用的节点保留为 `pinned-stale` 并产生提醒尽快解除引用的 warning。Nodes 页显示对应 badge，订阅页逐节点列出 stale 名称、所属订阅与 Route 引用；被引用节点只禁用自身 clean。

同一 App 生命周期只执行一次初始 Load，因此关闭主窗口再从菜单栏打开会保留内存中的 Draft。Reload、安装/Repair 和退出如果遇到 dirty Draft，统一进入 Save / Discard / Cancel guard；Cancel 不触碰 Draft、Saved 或 Active。安装完成不再无条件 Load：Save 会保留并写入安装前的 Draft，Discard 才明确以安装后的 Saved 配置替换它。

系统配置的唯一真相仍是：

```text
/Library/Application Support/Steer/config/config.json
```

## DNS 路径

TUN 启用 `dns_mode: hijack`；macOS CLI 不会自行配置系统 DNS，因此 `_run` 在核心 DNS 可用后用 `networksetup` 管理物理网络服务 DNS。按服务 UUID 保存原设置，自动 DNS 恢复为 `Empty`，手动 DNS 恢复原地址。每 5 秒接管新增服务；对用户后续修改放弃所有权。VPN 专用服务和搜索域不修改。

macOS 不复制 Linux 的 nftables `PREROUTING`/`OUTPUT` shim，也不引入 SmartDNS。DNS 由同一份 sing-box DNS Router 处理，Steer 在 TUN inbound 上只对明确的 TCP/UDP 目标端口 53 生成 `hijack-dns` 规则：

```text
应用 / 系统 resolver → 198.18.0.2 / fdfe:dcba:9876::2
        ↓
macOS utun
        ↓
sing-box route: inbound=steer-tun, tcp/udp, port=53
        ↓
sing-box DNS Router
        ↓
DNS Profile → Route / outbound
```

明确端口规则在 1.14 的 TUN 预匹配阶段进入逐包 DNS 处理；不使用协议嗅探作为 DNS 劫持条件。DNS Profile、缓存、detour 和上游协议继续由 sing-box 内部实现。Bootstrap 只解析 DNS 上游等基础设施主机名；Direct UDP/TCP Bootstrap 可产生明文 53，但不携带原始业务查询名。应用自带 DoH/DoT/DoQ 是普通业务流量，只有进入 Steer TUN 且另有可验证策略时才可能控制；port-53 hijack 本身不能识别或重定向它。

GUI 的 Diagnostics 只检查当前发布 generation 是否包含预期 `inbound=steer-tun + tcp/udp + destination port 53 + hijack-dns` 配置，并显示静态 exclusions。它不是抓包观测，也不证明零泄漏。loopback、link-local、multicast、文档和其他保留地址继续排除；为保证网络稳定，不扩大为无差别本地链路劫持。

## Go macOS adapter

`go/internal/platform/macos` 负责：

- Darwin TUN plan：地址、MTU、`auto_route` 和非全球地址排除；`198.18.0.0/15` 包含 system stack 自身的 IPv4 对端，不能加入排除表；
- 显式 TCP/UDP 53 DNS capture；
- sing-box version/capability/check；
- generation prepare/publish；
- launchd stop/bootstrap；
- utun 地址、LaunchDaemon 和当前 generation 的 DNS 接管 health；
- `_run` 看护 sing-box，先恢复 DNS 再结束核心；control daemon 回收崩溃后遗留的 `state/dns-restore.json`，卸载在删除 helper/state 前再次恢复；
- atomic Apply record、status 和 cleanup。

默认运行目录为：

```text
/Library/Application Support/Steer/config/config.json
/Library/Application Support/Steer/run/
/Library/Application Support/Steer/state/
/Library/Application Support/Steer/geodata-seed/manifest.json
/Library/Application Support/Steer/geodata-seed/rules/*.srs
```

`_run` 只供 LaunchDaemon 使用。它在冷启动时准备 current generation，然后直接 `exec` sing-box，不成为第二个 supervisor。

## Release 安装

稳定或预发布 tag 在两个原生 GitHub macOS runner 上分别构建：

```text
steer-macos-arm64.dmg
steer-macos-x86_64.dmg
```

DMG 内的 `Steer.app` 包含同架构 Swift GUI、`steer-macos`、SagerNet 官方 sing-box、运行/control/订阅调度三个 LaunchDaemon plist、完整 Geo seed、许可证和 embedded installer。构建过程严格校验上游 archive SHA、Mach-O 架构、版本/tags/revision、Geo manifest、helper validate/parse-nodes、bundle 布局与可执行权限，然后对嵌套二进制和 App 做 ad-hoc 签名并运行 `codesign --verify --deep --strict`。

项目目前没有付费 Apple Developer/Developer ID，因此 DMG **没有公证**。ad-hoc 签名只保证 bundle 在构建后未被意外改写，不能让 Gatekeeper 自动放行。用户流程是：

1. 从 GitHub Release 下载与本机架构匹配的 DMG，并校验 `SHA256SUMS`；可选运行 `gh attestation verify steer-macos-arm64.dmg -R gsh20040816/steer`。
2. 把 `Steer.app` 拖入 `/Applications`，按 macOS 的“未认证开发者”流程手动确认首次打开。
3. 在“系统”页点击“安装系统组件”，输入一次管理员密码。
4. 后续从 GUI 保存、Apply、启停和升级配置时不再重复输入密码。

“系统”页不会仅凭 helper 存在就声称完整安装。任一必需文件、LaunchDaemon、配置、Geo manifest 或 control socket 缺失，以及 embedded payload 版本不一致，都会列出具体事实并显示 Repair。Repair 仍只执行 App 内固定安装器，默认保留 config/state，完成后自动重新验收。

“卸载系统组件”只执行 App 内固定卸载器：先 bootout 三个 LaunchDaemon，再删除 helper、sing-box、plist、control socket、runtime 与 Geo 程序组件。默认保留 `/Library/Application Support/Steer/config`、`state` 和 `/Library/Logs/Steer`；“同时删除用户数据”位于独立的第二次破坏性确认中。卸载器不接受路径或命令参数，重复执行安全。

artifact attestation 证明文件来自对应 tag workflow，但不会替代 Developer ID 或 notarization，也不会自动改变 Gatekeeper 判断。

## 源码开发安装

先安装 sing-box，再安装 helper 和 LaunchDaemon：

```sh
brew install sing-box
sudo macos/scripts/install-launchdaemon.sh
```

开发安装器把 `command -v sing-box` 选中的构件复制到 root-owned `/usr/local/libexec/steer/sing-box`，并安装运行、常驻 control 与订阅调度三个 LaunchDaemon。它只服务源码开发；正式 App 使用 `Contents/Resources/Installer` 的固定 payload，不依赖 PATH，也不在用户机器上运行 `go build`。

构建 GUI：

```sh
cd macos
swift build --disable-sandbox
swift run SteerApp
```

也可以直接使用 helper：

```sh
sudo /usr/local/libexec/steer/steer-macos validate
sudo /usr/local/libexec/steer/steer-macos apply
sudo /usr/local/libexec/steer/steer-macos health
sudo /usr/local/libexec/steer/steer-macos status
```

## 限制

当前 macOS 目标明确不支持：

- `source_mac_address`；
- Linux `auto_redirect`、nftables、pf；
- SmartDNS 独立进程；
- 应用自带 DoH/DoQ 的明文查询识别。

Geo 表达式可以使用。正式 DMG 内置与当前 tag workflow 匹配并完整验证的 `geodata-seed/`；Geo 转换在 CI/release 阶段完成，目标机不安装 geoview，也不读取 DAT。

当前分发不声称 Developer ID 签名或 notarization；如果未来取得签名凭据，可以在不改变 embedded payload、control IPC 和 canonical 配置语义的前提下加入正式签名与公证。

系统 DNS 接管的边界：管理启用的 Ethernet/IEEE80211 网络服务；不抢占 VPN 的专用 resolver。启动先验证 IPv4/IPv6 DNS 地址路由到具有 Steer 地址的本机 utun，再验证 DNS 回复和默认 resolver 使用 Steer 地址；未生效则撤销接管并报告失败。避免路由器 DNS 劫持导致的远端回复被误判为本机核心就绪。DNS 入口连续三次健康探测失败也会恢复原设置并退出。系统 DNS 的持久 journal 保证异常退出后可恢复；独立控制后台被同时停止时，需要重新启动或运行 `steer-macos cleanup` 执行恢复。
