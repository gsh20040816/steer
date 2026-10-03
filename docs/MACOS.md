# macOS

macOS 版分两部分：

- **SwiftUI 图形界面**：和 OpenWrt 的 LuCI、Linux 的网页界面地位相同，只负责编辑配置、执行操作和显示状态，本身不转发任何流量。
- **后台服务**：几个以 root 身份运行的 LaunchDaemon，加上外部的 sing-box，负责真正接管流量。

```text
Steer 图形界面
  ├── 编辑配置
  ├── 读取 / 校验 / 查看状态（不需要密码）
  ├── 首次「安装系统组件」
  │          ↓ 输入一次管理员密码
  └── 保存 / 应用 / 更新或清理订阅（之后不再要密码）
             ↓ /var/run/steer/control.sock
root 后台服务：steer-macos _control
             ↓ 只允许固定的几种操作
/usr/local/libexec/steer/steer-macos
             ├── launchctl bootstrap → root 后台服务：steer-macos _run
             └── 每 15 分钟 → root 后台服务：subscription update
root 后台服务：steer-macos _run
             ↓ 作为子进程启动并看护
sing-box run -c current/sing-box.json
             ↓
macOS utun 虚拟网卡 + auto_route
```

整条路径不需要付费的 Apple 开发者账号。sing-box 官方支持在 macOS 上使用 TUN 入站；`auto_redirect` 只在 Linux 上有，macOS 不用。参见 [sing-box Tun 文档](https://sing-box.sagernet.org/configuration/inbound/tun/)。

## 三份配置：Draft、Saved、Active

界面里会反复出现这三个词：

- **Draft**：界面里正在编辑、还没保存的配置。
- **Saved**：已经写入 `/Library/Application Support/Steer/config/config.json` 的配置。这是唯一的权威配置。
- **Active**：sing-box 当前实际在运行的配置。

## 图形界面

`macos/SteerApp` 直接调用系统里安装的 `steer-macos`，不自己实现配置的含义。各页面：

- **总览**：显示 Draft/Saved/Active 三者是否一致、当前 Draft 里六类对象各有多少、校验错误和警告、最近一次应用的结果，以及常用操作。不显示内部的 generation 编号、摘要值或原始错误链。
- **基础设置**：编辑全局选项、测试网址、DNS 缓存和引导 DNS。
- **节点、路由、DNS、规则、订阅、本地代理**：用表格和表单编辑，可以拖动排序。列表只显示名称，不显示内部 ID。
- **高级（JSON）**：直接编辑完整配置，用于整体导入、排查问题或编辑表单里没有的字段。
- **诊断**：显示校验结果、最近一次应用、网络测试和结果摘要、DNS 接管检查，以及按需加载的运行日志。节点和路由的最近测速结果显示在各自旁边，不保留历史。
- **系统**：逐项列出后台程序、sing-box 版本和编译选项、当前运行的配置、最近一次应用、Geo 数据版本和规则数、三个 LaunchDaemon、DNS 接管范围、配置文件和 control socket 是否就位。缺东西或版本不一致时可以点「修复」，也可以卸载。

工具栏和菜单栏在所有页面都有这几个按钮：

- **保存**：把 Draft 写入 Saved。
- **应用已保存**：从磁盘重新读取 Saved 再应用。即使 Draft 有未保存的修改，也不会带进去或被覆盖。应用失败时，新配置不会被当作 Active。
- **保存并应用**：先保存再应用。
- **启用开关**：只修改 Saved 里的 `enabled` 并立即应用，不动 Draft；Draft 里未保存或格式有误的修改都会原样保留。失败时，开关显示 Saved 里的期望状态，运行状态标记显示 sing-box 的真实状态。

读取配置、查看状态、校验、网络测试和查询 Geo 分类都不需要密码。配置文件权限是 `root:admin 0640`；当前运行配置的摘要文件 `current.json` 不含密钥，界面可以直接读。

网络测试也通过 control socket 交给 root 后台服务执行。请求里只有测试类型、节点或路由 ID、是否测下载这几个字段，不能传网址、路径或命令。后台服务从 Saved 配置里选择测试目标，直接用 Mac 当前的网络访问，所以就算 sing-box 没在运行也能测。

### 后台服务接受什么

control 后台服务只接受这些请求：`status`、`state`、`set-enabled`、`save`、`apply`、`probe`、`diagnostics`、`probe-results`、`subscription-update`、`subscription-clean`。请求格式固定、大小有上限，不能传 shell 命令、网址、路径或可执行文件。

需要特别注意的是：任何管理员组用户都能免密码让这个 root 服务保存配置，而 Tor 节点本来允许指定 Tor 程序路径和启动参数。为了不让它变成「以 root 运行任意程序」的入口，macOS 版会拒绝带有 `executable_path`、`extra_args` 或 `data_directory` 的 Tor 节点，只能使用 sing-box 默认的 Tor 设置。

其他安全措施：

- socket 所在目录归 root 所有，普通管理员无法替换；socket 本身权限是 `root:admin 0660`。
- 服务端会再用 `LOCAL_PEERCRED` 确认调用者是 root 或管理员组成员。
- 写入的配置同样经过严格解码和完整校验，再以 `root:admin 0640` 原子替换。
- 测试结果只把摘要返回给界面，原始测量数据和完整错误链不会传过去。

界面不直接写运行配置，也不直接启动 sing-box。为了在表单里即时提示，界面对端口、必填项、地址等做了少量本地检查，但最终以后台服务的校验结果为准。

### 并发修改保护

界面每次读取配置时会记下内容的 SHA-256（revision）。保存和应用时必须带上这个值。后台服务在操作锁（和订阅定时任务共用）里先比较当前 Saved 的 revision，一致才写入。如果不一致，返回 `REVISION_CONFLICT`，Saved、Active 和 Draft 都不变。

这时界面给出三个选择：重新读取 Saved、保留本地 Draft、强制覆盖。强制覆盖会用冲突响应里的最新 revision 再比较一次，不会跳过保护。

### 订阅

订阅的定时更新和手动更新都只修改 Saved 里的节点，从不自动应用。

- 如果手动更新期间 Draft 没变，更新完成后会自动重新加载。
- 如果用户在更新过程中继续编辑，界面保留 Draft 并给出上面的冲突选择，不会悄悄用更新结果覆盖编辑内容。

订阅列表会同时显示最近一次成功和最近一次失败。上游删掉的节点，如果没有路由在用，更新时直接删除；如果有路由在用，就保留并标记为「过期（pinned-stale）」，同时给出警告，提醒尽快改掉引用。节点页会显示过期标记；订阅页会列出过期节点的名称、所属订阅和引用它的路由。被引用的节点不能单独清理。

### 未保存修改的保护

应用启动后只自动读取一次配置。关掉主窗口再从菜单栏打开，Draft 仍保留在内存里。

重新读取、安装或修复组件、退出应用时，如果 Draft 有未保存的修改，会询问「保存 / 放弃 / 取消」。选「取消」什么都不改。安装完成后不会自动覆盖 Draft：选「保存」会把安装前的 Draft 写入，选「放弃」才会换成安装后的 Saved。

## DNS

TUN 设置了 `dns_mode: hijack`。sing-box 不会自己修改 macOS 的系统 DNS，所以 `_run` 在 sing-box 的 DNS 可用后，用 `networksetup` 修改物理网络服务的 DNS：

- 按服务 UUID 记下原来的设置。原来是自动获取的，恢复成 `Empty`；原来是手动填写的，恢复成原地址。
- 每 5 秒检查一次，新出现的网络服务也会被接管。
- 如果用户之后自己改了某个服务的 DNS，Steer 就不再管它。
- 不修改 VPN 专用的服务，也不修改搜索域。
- 只接管已启用的以太网和 Wi-Fi 服务。

DNS 健康检查会确认 IPv4/IPv6 的 DNS 地址确实路由到 Steer 的 utun 网卡、DNS 能正常回复、系统默认解析器用的是 Steer 的地址。这样可以避免路由器劫持 DNS 时，把远端的回复误当成本机 sing-box 已就绪。

检查失败时，sing-box 继续运行，每 5 秒重试一次，失败原因写入日志和 `dns-ready.json`。**目前检查失败不会撤销 DNS 接管**：如果 sing-box 的 DNS 一直不通，系统 DNS 会一直指向 198.18.0.2，需要手动停用 Steer。接管记录会持久保存，所以进程异常退出后可以恢复原设置；如果 control 后台服务也同时被停掉，需要重启电脑或运行 `steer-macos cleanup` 来恢复。

macOS 没有 Linux 那样的 nftables 规则，也不用 SmartDNS。所有 DNS 都由 sing-box 自己的 DNS 模块处理。Steer 只在 TUN 入站上为目标端口 53 的 TCP/UDP 流量加一条 `hijack-dns` 规则：

```text
应用 / 系统解析器 → 198.18.0.2 / fdfe:dcba:9876::2
        ↓
macOS utun
        ↓
sing-box 路由：inbound=steer-tun，tcp/udp，端口 53
        ↓
sing-box DNS 模块
        ↓
DNS 配置 → 路由 / 出口
```

几点说明：

- 劫持只看端口，不靠协议嗅探判断是不是 DNS。
- 引导 DNS 只用来解析 DNS 服务器等基础设施的域名。直连的 UDP/TCP 引导 DNS 会产生明文的 53 端口查询，但里面不包含用户实际访问的域名。
- 应用自己发起的 DoH/DoT/DoQ 是普通的加密流量，端口 53 劫持识别不了，也改不了它们。
- 进入 TUN 的 ping（ICMP echo）在嗅探前就按规则路由：保留规则顺序、WireGuard 和拒绝；本应走代理的目标会改走直连，因为代理协议不能转发 ping。

诊断页里的 DNS 检查只确认当前运行的配置里有这条劫持规则，并列出不接管的地址范围。它不是抓包，不能证明没有 DNS 泄漏。回环地址、链路本地地址、组播、文档地址和其他保留地址都不接管，避免影响本地网络。

## Go 适配器

`go/internal/platform/macos` 负责：

- TUN 网卡的地址、MTU、`auto_route` 和不接管的地址范围。注意 `198.18.0.0/15` 里包含系统网络栈自己的 IPv4 对端地址，不能放进排除列表。
- 端口 53 的 DNS 劫持规则和 ping 的路由。
- 检查 sing-box 的版本和编译选项，让它校验生成的配置。
- 生成和发布运行配置（generation）。
- 用 launchd 停止和启动后台服务。
- 检查 utun 地址、后台服务和 DNS 接管是否正常。
- `_run` 作为子进程启动 sing-box 并看护它；停止时先恢复 DNS 再结束 sing-box。control 后台服务会处理崩溃后留下的 `state/dns-restore.json`；卸载时，在删除程序和状态目录前也会再恢复一次 DNS。
- 记录每次应用的结果、读取状态、清理。

默认目录：

```text
/Library/Application Support/Steer/config/config.json
/Library/Application Support/Steer/run/
/Library/Application Support/Steer/state/
/Library/Application Support/Steer/geodata-seed/manifest.json
/Library/Application Support/Steer/geodata-seed/rules/*.srs
```

`_run` 只给 LaunchDaemon 用。它的 plist 设置了 `RunAtLoad`，所以开机或安装时总会被加载一次；如果 Saved 里 `enabled=false`，它会直接退出。因此 Steer 停用时，「系统」页可能显示这个后台服务已加载但没有在运行，这是正常的，不需要修复。判断安装是否完整只看程序、sing-box、三个 plist、配置文件、Geo 数据、control 和订阅两个后台服务以及 control socket。

## 从 Release 安装

稳定版和预发布版在 GitHub 的 `xcode-27` Apple Silicon 机器上用 Xcode 27.0 和 macOS 27 SDK 构建，产物是：

```text
steer-macos-arm64.dmg
```

DMG 里的 `Steer.app` 包含：图形界面、`steer-macos`、SagerNet 官方 sing-box、三个 LaunchDaemon 的 plist（运行、control、订阅定时）、完整的 Geo 数据、许可证和安装器。构建时会校验上游压缩包的 SHA、二进制架构、sing-box 版本/编译选项/提交、Geo 数据清单，还会实际运行一遍 `steer-macos` 的校验和节点解析，检查目录结构和可执行权限，最后对内部程序和整个应用做 ad-hoc 签名并用 `codesign --verify --deep --strict` 验证。

项目没有付费的 Apple 开发者账号，所以 DMG **没有公证**。ad-hoc 签名只能保证构建后文件没被改动，Gatekeeper 不会因此自动放行。安装步骤：

1. 从 GitHub Release 下载 DMG，核对 `SHA256SUMS`。也可以运行 `gh attestation verify steer-macos-arm64.dmg -R gsh20040816/steer` 确认它来自本仓库的发布流程。
2. 把 `Steer.app` 拖进「应用程序」，按 macOS 打开未认证开发者应用的步骤手动确认。
3. 在「系统」页点「安装系统组件」，输入一次管理员密码。
4. 之后保存、应用、启停和更新配置都不再需要密码。

构件证明（attestation）只能证明文件来自对应 tag 的构建流程，不能代替 Developer ID 签名或公证，也不会改变 Gatekeeper 的判断。

「系统」页不会因为程序文件存在就认为安装完整。任何必需文件、后台服务、配置、Geo 清单或 control socket 缺失，或者应用内附带的组件版本对不上，都会逐项列出并提供「修复」。修复只运行应用内固定的安装器，默认保留配置和状态，完成后会重新检查一遍。

「卸载系统组件」只运行应用内固定的卸载器：先停掉三个后台服务，再删除程序、sing-box、plist、control socket、运行目录和 Geo 数据。默认保留 `/Library/Application Support/Steer/config`、`state` 和 `/Library/Logs/Steer`；要连这些一起删，需要在第二次确认里单独勾选。卸载器不接受路径或命令参数，重复运行也没问题。

## 从源码安装（开发用）

先装 sing-box，再装后台程序和 LaunchDaemon：

```sh
brew install sing-box
sudo macos/scripts/install-launchdaemon.sh
```

这个开发用安装器会把 `command -v sing-box` 找到的 sing-box 复制到 root 所有的 `/usr/local/libexec/steer/sing-box`，并安装三个 LaunchDaemon。正式应用使用的是 `Contents/Resources/Installer` 里的固定文件，不依赖 PATH，也不会在用户电脑上编译。

构建图形界面：

```sh
cd macos
swift build --disable-sandbox --build-system native
swift run --build-system native SteerApp
```

也可以直接用命令行：

```sh
sudo /usr/local/libexec/steer/steer-macos validate
sudo /usr/local/libexec/steer/steer-macos apply
sudo /usr/local/libexec/steer/steer-macos health
sudo /usr/local/libexec/steer/steer-macos status
```

## 不支持的功能

- 按来源 MAC 匹配（`source_mac_address`）。
- IPv6 内核直连（`direct_bypass`）。
- Tor 节点的自定义程序路径、启动参数和数据目录。
- Linux 的 `auto_redirect`、nftables，以及 macOS 的 pf。
- 独立的 SmartDNS 进程。
- 识别应用自带的 DoH/DoQ 查询。

Geo 规则可以正常使用。DMG 里附带的 `geodata-seed/` 和发布流程完全匹配并经过校验；Geo 格式转换在 CI 里完成，用户电脑上不装 geoview，也不读 DAT 文件。

目前的发布版没有 Developer ID 签名和公证。以后如果有了签名证书，可以直接加上签名和公证，不需要改动安装包内容、后台服务接口或配置格式。
