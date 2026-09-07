# 测试说明

测试按共享核心、OpenWrt/Linux/macOS 适配器、LuCI/Linux Web/macOS GUI 和目标系统正常路径分层。

## Go

```sh
cd go
go test -race ./...
go vet ./...
```

覆盖 schema 9、严格 JSON/UCI 解码、引用和前置链校验、确定性编译、Route 私有出站、DNS 路径、remote SRS/seed manifest、共享 Apply 生命周期、generation、订阅合并/Store、probe 测量，以及 OpenWrt 计划、nftables、激活、健康和日志。

Linux 适配器测试覆盖主机与转发流量 plan、原生 DNS 接管、本机目的地址 PREROUTING/OUTPUT 例外与受保护的 wildcard listener、1.14 原生 source-MAC、JSON 原子写入与 ETag 冲突、systemd/backend generation、Web bearer token/CSP/开关失败回滚、临时 probe 的 bypass mark 和静态 Linux 构建。

macOS 适配器测试覆盖 Darwin TUN plan、TUN port-53 capture、JSON store、generation、launchd backend 和平台限制。
SwiftUI 工作副本行为由 `cd macos && swift test --disable-sandbox` 覆盖，包括规则 string-list 的无损逐行 round-trip 与 Default 固定不变量。

## LuCI 与静态边界

```sh
node tests/node/luci_view_test.js
node tests/node/steer_helper_test.js
node tests/node/linux_web_test.js
python3 tests/check-luci-i18n.py
python3 tests/check-package-boundaries.py
python3 tests/check-build-cache.py
python3 tests/check-linux-packaging.py
python3 tests/check-macos-packaging.py
python3 tests/check-ui-contract.py
```

macOS 原生表单还包含 Swift XCTest：

```sh
cd macos
swift test --disable-sandbox
```

- `internal/subscription` Go 测试：三端共享的代理 URI、多行与 Base64 解析，以及逐协议非丢失导出闭环；
- `subscription-status-fixtures.json`：三端共享的 never-fetched、success、skipped、failed-after-success、disabled 与部分 stale 引用状态；
- `probe-diagnostics-fixtures.json`：三端共享的内部 sanitized probe 事实、普通 UI 最近结果摘要边界、Active port-53 配置检查与 enabled/disabled 操作事实；
- `state-lifecycle-fixtures.json`：三端共享的 fresh、pending-disable、failed-Apply 与 active Draft/Saved/Active 状态；
- `validation-issue-fixtures.json`：三端写失败的完整问题定位与无凭据消息合同；
- `collection-reference-fixtures.json`：Node/Route/DNS/Local Proxy/Subscription 删除引用保护合同；
- `rule-summary-fixtures.json`：完整 Rule match 摘要与 DNS/连接阶段边界合同；
- `form-input-fixtures.json`：三端共享的 Probe/Subscription URL、正 duration 与 DNS HTTP path 格式合同；
- `macos-system-component-fixtures.json`：macOS helper、sing-box、plist、LaunchDaemon、config、Geo seed 与 control socket 的逐组件缺失/版本事实，并区分安装必需事实与 runtime 激活状态；
- `luci_view_test.js`：表单语义、逐 RPC 权限、Local Proxy 暴露/认证门、detour 可清空、节点/路由测试按钮，以及 Overview 渲染与响应式布局；
- `steer_helper_test.js`：UCI commit 后的 Apply 阶段反馈、结构化错误本地化、独立 validate 与 session access；
- `linux_web_test.js`：Linux Web 开关/冲突回滚、Overview 五区域与安全 Apply 摘要、Rules/Node chips 与 SSH 私钥、Local Proxy 认证 DOM 往返、Advanced JSON 单一 Draft、无效 JSON 导航保护及确认式 Discard；
- Swift XCTest：共享 Local Proxy、订阅、Probe、Overview Saved/Active 生命周期、状态 fixture 与 macOS 提交门；
- Python 检查：生成规格与 LuCI 菜单一致性、中文覆盖、包所有权/版本、官方 SDK 缓存与 Linux 交付约束。

## OpenWrt VM

`tests/integration/run-openwrt-vm.sh` 只能运行在一次性 OpenWrt 25.12.5 x86/64 VM，并要求匹配版本的 sing-box、完整 SRS seed 和待测控制器已准备。脚本会改写 `/etc/config/steer`、`/run/steer`、nftables、策略路由和 procd，不能用于生产路由器。

它覆盖公共 RPC 集、Geo catalog、通过真实 ucode RPC 的单条/多行/Base64 节点导入、代表配置校验、正常 Apply、LuCI UCI commit 触发、health/status、DNS shim 与 1.14 原生 MAC 规则、fw4 reload、服务 restart/reload、非法字段 fail-fast、禁用和重新启用。编译器的详细结构由 Go 测试覆盖，不为测试重新公开 compile/plan/prepare 命令。

## Linux systemd 容器

`tests/integration/run-linux-system.sh` 只能运行在显式设置 `STEER_LINUX_SYSTEM_TEST=1` 的一次性 privileged systemd 容器，不能用于生产主机。发布 CI 使用 `tests/integration/linux-system.Dockerfile` 构建固定 Debian 环境，挂载本次构建的 Steer、同次验证的 SRS seed 和校验过 SHA-256 的 sing-box 1.14.0 musl 二进制。

CI 只对容器基础设施启动做有限重试：镜像只构建一次，容器最多重建 3 次，每次最多等待 30 秒，必须等到 systemd 可响应且 `systemd-resolved` active；每次失败输出容器状态、failed units 和本次 boot journal。真正的 `run-linux-system.sh` 产品集成只运行一次，失败不会重试或被掩盖。

脚本建立 upstream/client 两个 netns，固定 client MAC 后先验证隔离拓扑，再把默认路由切到无公网出口的 upstream。启用配置实际引用 `geosite:cn` 和 native `source_mac_address`，所以服务在 Pages 不可达时仍必须通过包内 `initial_path` 启动；随后覆盖主机和转发流量的 IPv4/IPv6 TCP、UDP、UDP/TCP53。DNS 请求使用不存在的原目标地址，只有经过 Steer redirect 和独立 DNS upstream 才能成功；测试同时检查本机目的地址的 nft DNS counter 增长，并确认 `steer0` 已注册原生 systemd-resolved DNS。它还确认 1053/1054 不能被直接当作 LAN resolver 访问，并覆盖服务重启、`nftables.service` 重启、禁用和重新启用。

## 发布前完整检查

```sh
cd go && go test -race ./... && go vet ./...
cd ..
node tests/node/luci_view_test.js
node tests/node/steer_helper_test.js
node tests/node/linux_web_test.js
python3 tests/check-luci-i18n.py
python3 tests/check-package-boundaries.py
python3 tests/check-build-cache.py
python3 tests/check-linux-packaging.py
sh -n tests/integration/run-openwrt-vm.sh
sh -n tests/integration/run-linux-system.sh
git diff --check
```

所有分支 commit 与 PR 都运行 Ubuntu/Linux/OpenWrt 测试、Linux systemd 容器集成，以及 arm64/x86_64 原生 macOS Go/Swift 测试；CI 不设置 concurrency 限制，也不保存正式发布包。tag commit 必须进入 master；稳定版另要求同一 SHA 的 master CI push run 成功，Actions 服务降级时预发布可使用完整本地发布门。tag push 事件丢失时允许显式 dispatch 同一 tag，但 branch ref 会被 source gate 拒绝。OpenWrt SDK、Linux 归档、原生 macOS DMG、attestation 和发布仍全部在同一次 tag workflow 中完成。

### 隔离验证原生 DNS 接管

导出由当前 Linux/OpenWrt plan 生成的测试配置和防火墙（仅使用内置 hosts 数据，不访问公网）：

```sh
cd go
STEER_DNS_FIXTURE_DIR=/tmp/steer-dns-fixtures go test ./internal/platform -run TestExportNativeDNSFixtures
```

把相应平台的 `.json`、`.nft` 和 `tests/integration/check-native-dns.py` 放到测试机后运行：

```sh
# Linux 用户命名空间，无需 sudo
unshare -Ur python3 check-native-dns.py linux.json linux.nft
# OpenWrt root
python3 check-native-dns.py openwrt.json openwrt.nft
```

脚本先创建独立 network/mount namespace，并屏蔽宿主 D-Bus；不会改宿主路由、DNS 或现有 Steer 服务。覆盖本机/转发 IPv4/IPv6 UDP/TCP53、本机目的地址兼容 shim（含本机查询 127.0.0.1、127.0.0.53、::1 和自身 IPv4/IPv6 地址），以及同一 UDP 源端口从 DNS 复用到 STUN 的回归测试。需要 Python 3、ip-full、nft、dig、mount 和支持 namespace/TUN 的内核。

macOS 的 DNS journal 测试覆盖自动/手动 DNS 恢复、服务重命名、新增服务、用户后续修改、部分写入失败、恢复失败重试和核心退出/取消。`STEER_TEST_SYSTEM_DNS_READ=1 go test ./internal/platform/macos -run TestReadSystemDNSPreferences -v` 可只读验证本机物理网络服务发现；此检查不修改系统 DNS。

## OpenWrt netlink 故障恢复

`check-netlink-guard.py <steer-openwrt> <sing-box>` 使用独立网络和挂载命名空间，暂停测试核心并注入路由通知积压，验证持续烧核后的自动恢复、十分钟冷却和停止时子进程退出。它不使用生产配置。非初始命名空间的 `rmem_default` 通常只读，此时仍验证降级恢复；接收缓冲提升需另在可写环境检查 netlink diag 返回的实际 socket 缓冲及系统默认值恢复。CI 将此测试与 DNS 集成一起执行。
