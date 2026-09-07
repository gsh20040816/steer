# 架构

Steer 采用“共享意图核心 + 平台适配器”。共享编译器生成可直接交给 sing-box 的最终配置；平台适配器提供该平台所需的入站片段，并单独规划和应用操作系统资源。

## 代码边界

```text
go/internal/intent                 Canonical Intent、schema 9 校验与严格 JSON codec
go/internal/compiler               确定性 sing-box 最终配置编译
go/internal/apply                  同步 Apply 编排和结果合同
go/internal/generation             平台中立 generation 文件
go/internal/subscription           抓取、解析、合并与窄 Store 接口
go/internal/probe                  HTTP/TLS 测量和报告
go/internal/capability             sing-box 版本/build-tag 能力检查
go/internal/platform/openwrt       OpenWrt 网络、服务、UCI、Geo 和日志适配
go/internal/platform/linux         systemd、TUN、DNS nft shim、JSON 和 Linux 状态适配
go/internal/platform/macos         launchd、Darwin TUN、JSON 和 macOS 状态适配
go/internal/platform/openwrt/uci   严格 UCI 语法解析
go/cmd/steer-openwrt               OpenWrt CLI 源码 target
go/cmd/steer-linux                 Linux CLI 和 loopback Web 控制面
go/cmd/steer-macos                 macOS helper 和 LaunchDaemon 入口
macos/SteerApp                     macOS SwiftUI 配置与运维前端
go/internal/geodata                包内 SRS seed manifest 与文件完整性校验
```

共享包只能依赖共享包；`platform/openwrt` 和 `platform/linux` 可以依赖共享包，反向依赖禁止。

## 数据流

```text
OpenWrt UCI ──严格解码──┐
Linux JSON ──严格解码───┼→ Canonical Intent → Validate → Compiler
macOS GUI/JSON ──────────┤
                       │                         │
                       └─────────────────────────┘
                                                   ↓
                                      final sing-box config
                                                   +
                                      platform-native plan
                                                   ↓
                                   Prepare / Activate / Healthy / Finalize
```

编译结果由 Intent、明确状态目录和平台提供的 sing-box Target 决定。时间、探测结果、订阅抓取状态和操作系统运行信息不会混入 Intent 或编译结果。

## 路由与 DNS

`route.detour` 是 single Route 到另一条 single Route 的有向边：

```text
应用流量 → exit route/node → front route/node → 网络
```

校验器拒绝悬空、禁用、非 single 目标和任意环。编译器为每条 single Route 生成独立协议出站 `steer-route-<id>`，因此同一节点可以在不同 Route 中拥有不同前置链，不需要全局节点 selector。

每个启用规则实际引用的 `(DNS Profile, Route)` 编译成独立 DNS transport。代理 Route 引用同一个 Route tag，自动继承完整前置链；Direct 不写 detour；Reject（兼容键 `kind=block`）在业务路由和 DNS 路由中都投影为 `action: reject`，不创建 block outbound。普通规则与 DNS 规则共享可表达的匹配条件，目标 IP、网络、协议和端口只参与业务流量。

## OpenWrt 数据面

主路径由 sing-box 1.14 TUN 的 `auto_route`、`strict_route`、`auto_redirect` 和 `dns_mode: hijack` 接管，包括普通 DNS 重定向。Steer 仅保留本机目的地址的 DNS 例外：PREROUTING 拦截 LAN 查询路由器自身，OUTPUT/SNAT 保留路由器本机查询本地 DNS 的原有覆盖。其他 DNS 重定向交给 sing-box，删除未使用的 nft 地址集合。路由器本机 UDP/123 仍明确直连。

源 MAC 条件由 sing-box 1.14 的 `source_mac_address` route/DNS rule 原生匹配，不再创建专用 TProxy/MAC DNS 入口或策略路由。TUN 名称、地址、table、priority、mark、NFQUEUE 和兼容 DNS 端口由平台管理，不出现在 Canonical Intent。

## Linux 数据面

Linux 支持 systemd 主机及其 VM/Docker 转发流量，不设置 `include_interface` 限制。TUN 启用 `dns_mode: hijack`，sing-box 负责普通 TCP/UDP 53 重定向和 systemd-resolved link DNS。Steer 将外部 PREROUTING/OUTPUT DNS 规则收窄到本机目的地址，并保留必要 SNAT 及专用入口访问保护，覆盖本机和 LAN 查询本地 DNS。源 MAC 条件由原生邻居解析匹配。不改 NetworkManager connection、`/etc/resolv.conf` 或 systemd-resolved drop-in，也不生成 MAC 策略路由。

`platform/linux` 固定自己的 TUN、DNS、table、priority、mark 和 NFQUEUE 资源；这些只保存在 generation 内部的 `platform.json`，不进入 Canonical Intent，也不存在用户可编辑的 `/etc/steer/platform.json`。Geo category 是共享 Intent 语义；两个 adapter 都使用包内 `/usr/share/steer/geodata-seed`。`internal/geodata` 以严格 manifest 精确解析 selector，并在切换运行态前校验所需 SRS 的普通文件类型、大小和 SHA-256。

编译器为用到的 Geo selector 生成 sing-box remote binary rule-set：包内 SRS 是 `initial_path`，Pages 上同名 SRS 是 `url`，更新周期为 1 天。显式 direct HTTP client 避免 Geo 下载受用户代理路由影响；`/var/lib/steer/cache.db` 由 sing-box 保存 remote rule-set cache。首次启动不依赖网络，远端更新失败只记录在 sing-box 日志中，不改变已通过 seed 启动的 Apply 结果。

Steer 不写全局 `dns.strategy`，也不写 DNS rule action 的 query-level `strategy`，客户端 A/AAAA 查询保持透明。SOCKS、HTTP 与 Mixed 本地入口的域名目标在 sniff 后先执行非终结的原生 `resolve` action；action 不固定 DNS server，而是复用 DNS Router 按用户规则选择的 `(DNS Profile, Route)` path，随后业务 route rule 使用解析结果。已有 IP 目标不会触发查询。DNS server 本身使用域名时，启动该 transport 所需的 `domain_resolver.strategy` 来自 `bootstrap.strategy`；route 默认域名解析器使用同一内部解析策略。schema 9 删除了 `dns_profile.strategy`。

## macOS 数据面

macOS 使用 LaunchDaemon 下的 sing-box Darwin TUN，不使用 pf 或 Network Extension。TUN 启用 `dns_mode: hijack`；`_run` 看护核心，在 IPv4/IPv6 UDP/TCP DNS 入口可用后，将物理网络服务的系统 DNS 指向派生地址 `198.18.0.2`、`fdfe:dcba:9876::2`。按服务 UUID 先持久保存原 DNS，区分自动与手动；停止、核心退出和卸载时恢复。独立 control daemon 每 5 秒恢复已停止运行态留下的 journal，覆盖 SIGKILL 和重启恢复。用户后续改变 DNS 时放弃该服务的所有权，不强行覆盖。VPN 专用服务和搜索域不修改。

TUN 使用默认公网路由和必要排除。已进入 TUN 的流量依次执行：① 明确 TCP/UDP 目标端口 53 `hijack-dns`；② 私网 Direct；③ sniff；④ resolve；⑤ 用户规则。不能保证应用硬编码的链路本地或直连 DNS 进入 TUN。网络轮询只接管新增物理服务，不读取 Saved、不创建 generation、不隐式 Apply。健康状态要求 DNS 地址经拥有 Steer 地址的本机 utun 路由、入口可用且当前 generation 的系统 DNS 接管已完成；这不代表所有 VPN 分域解析或加密 DNS 都经过 Steer。

## Apply

Linux 的 Apply、配置写入和订阅变更共用一把 operation lock；Apply 本身仍是同步流程：

1. 平台 codec 严格解码配置；
2. 共享校验器拒绝非法 Intent；
3. 共享编译器生成最终 sing-box 配置；
4. `Prepare` 检查 sing-box 能力、准备 Geo、生成候选并执行 `sing-box check` 与 `nft -c`；
5. `Activate` 由平台停止旧服务、应用平台资源、发布 `current`，再启动候选；
6. `Healthy` 检查 procd、TUN、nftables 和必要监听端口；
7. `Finalize` 删除其他运行 generation。

候选预检查失败发生在运行态变更前。`current` 在候选进程启动前发布，供 procd 读取。切换后的失败返回错误并保留现场；没有自动恢复、旧配置备份或 rollback 接口。禁用走单独的 `Disable`，停止服务并删除运行资源。

开机时 OpenWrt 由 procd 通过非公开 `_start` 钩子执行校验、编译、Prepare 和平台激活；Linux 由 systemd、macOS 由 LaunchDaemon 执行非公开 `_run`，必要时准备冷启动 generation，然后直接 `exec` sing-box。这些钩子都不是公共 CLI，也不构成第二套配置语义。macOS SwiftUI GUI 与 LuCI、Linux Web 一样，只通过平台后端读写 Canonical Intent 和触发公共生命周期。

## Generation 与状态

```text
/run/steer/generations/<id>/intent.json      Canonical Intent
/run/steer/generations/<id>/sing-box.json    最终核心配置
/run/steer/generations/<id>/platform.json    当前平台内部资源计划
/run/steer/generations/<id>/firewall.nft     当前平台 nftables 辅助层
/run/steer/current                            当前 generation 链接
/run/steer/last-apply.json                    最近一次公共 Apply 结果
```

Linux 的持久化真相是 `/etc/steer/config.json`，订阅 snapshot 和 Web token 位于 `/var/lib/steer`；运行时仍只写 `/run/steer` 和 `/var/lib/steer`，不会修改系统 resolver 配置。

Linux `status` 从 `current` generation 的实际 `intent.json` 与 `sing-box.json` 返回 Active 身份，并把最近 Apply 保持为独立操作记录：

```json
{
  "healthy": true,
  "generation": "candidate.…",
  "intent_digest": "…",
  "runtime_digest": "…",
  "last_apply": {
    "sequence": "…",
    "timestamp": "…",
    "result": {"ok": false, "candidate_generation": "/run/steer/generations/candidate.…", "activated": false, "error": "…"}
  }
}
```

`last_apply.result` 永远不用于推导 Active。Linux Web 的 `pending_apply` 比较 Saved 与 Active 的编译运行投影，而不是全文 Intent digest；因此未被 Route 使用的订阅节点库存刷新不会要求 Apply。

OpenWrt `status` 同样从 `current` generation 返回 Active generation、Intent digest、Runtime digest 与健康状态，`last_apply` 保持独立。LuCI 的 `_state` 内部接口只返回无凭据的 enabled/count/digest/validation 事实；当前 session candidate 与 committed UCI 分别读取，`pending_apply` 使用 Runtime digest，因此不会把订阅 inventory 误报成运行变更。

配置诊断由 `validate` 独立返回。组件细节留在平台健康检查和系统日志中，不扩张跨平台状态合同。

## 订阅与测试

共享订阅逻辑只依赖窄 `Store`：替换一组订阅节点或删除一个节点。OpenWrt Store 生成单次 UCI batch；JSON Store 使用 revision-guarded 原子写入。订阅只改变 Saved 节点库，提交后不主动 Apply。合并时自动删除上游已移除且无 Route 引用的节点，只把仍被 Route 引用的节点保留为 stale 并产生 warning。三端状态都由 `subscription.Status` 生成：最近成功 snapshot 与最近失败独立保存，失败摘要不含 URL/响应内容，stale 节点携带阻止 clean 的 Route 引用。UI 更新响应也使用该状态 DTO，不返回包含节点凭据的内部 snapshot。

共享 probe 负责 HTTP/TLS 测量、报告脱敏和受限持久化。原始 `Report` 是内部排错事实；普通控制面只公开按 `scope/object_id/kind` 唯一索引的 `LatestProbeResult`，字段限定为测试时间、成功/失败、后端计算的 stale、一个核心指标摘要、可选数值 `metric_value` 和一个安全错误摘要。读取按每个持久化键无损返回，不做跨对象的全局条数截断；同键写入使用跨进程锁和原子替换，较旧时间戳不得覆盖较新结果。Linux HTTP、OpenWrt ubus/`_probe-results` 与 macOS control/helper 暴露语义一致的批量 latest-result capability，测试动作本身也返回同一个 DTO，业务失败仍能立即呈现刚持久化的失败摘要。

Saved/Active identity、阶段耗时、URL、attempts 和完整错误只存在于后端报告与 stale/摘要计算过程，不进入 latest-result DTO。三端前端本地化 `tested_at`、显示摘要，并使用 `metric_value` 排序。连接指标单位为毫秒，下载指标单位为 Mbps；缺少指标的结果不参与数值排名。概览测试从各平台 Saved 配置读取三个固定 URL，直接使用设备当前网络环境访问，因此在 Steer 未启用时仍可运行；节点和路由测试读取 Saved 配置并临时启动环回 sing-box。概览请求只证明目标当时可达，不声称命中了某个 outbound 或 DNS resolver。测试结果不会进入配置或编译输入。

### OpenWrt netlink 恢复保护

procd 通过 `steer _supervise` 运行原版 sing-box。启动前临时将 `net.core.rmem_default` 提升至至少 4 MiB，五秒后恢复原值；已创建的 socket 保留较大的接收缓冲。调整失败时记录告警，核心仍可运行。

看护每五秒只检查子进程拥有的 IPv4/IPv6 路由通知 socket。单核 CPU 使用率至少 80%、接收队列至少 128 KiB 且连续不变、已有丢包，三项证据持续一分钟才恢复核心。十分钟冷却保存在 `/run/steer/netlink-recovery.json`，避免看护重启绕过限制。恢复使用当前运行配置，不读取或应用未生效的 Saved 修改；恢复原因写入系统日志。该保护是上游订阅失效的规避措施，不改变 sing-box 二进制。
