# 架构

Steer 分两层：共享的 Go 核心把用户配置（项目里叫 Canonical Intent）编译成可以直接交给 sing-box 的完整配置；各平台的适配器提供该平台需要的入站部分，并负责网卡、防火墙、系统服务这些操作系统资源。

## 代码目录

```text
go/internal/intent                 配置格式（schema 9）、校验、严格的 JSON 解析
go/internal/compiler               把配置编译成 sing-box 配置，结果是确定的
go/internal/apply                  应用流程和结果格式
go/internal/generation             运行配置（generation）里和平台无关的文件
go/internal/subscription           订阅的下载、解析、合并，以及精简的 Store 接口
go/internal/probe                  HTTP/TLS 测量和报告
go/internal/capability             检查 sing-box 版本和编译选项
go/internal/geodata                随包 Geo 数据的清单和文件完整性校验
go/internal/platform/openwrt       OpenWrt：网络、服务、UCI、Geo、日志
go/internal/platform/openwrt/uci   严格的 UCI 语法解析
go/internal/platform/linux         Linux：systemd、TUN、DNS 的 nftables 规则、JSON 配置、状态
go/internal/platform/macos         macOS：launchd、utun、JSON 配置、状态
go/cmd/steer-openwrt               OpenWrt 命令行
go/cmd/steer-linux                 Linux 命令行和本机网页界面
go/cmd/steer-macos                 macOS 后台程序和 LaunchDaemon 入口
macos/SteerApp                     macOS 图形界面
```

共享包只能依赖共享包；平台包可以依赖共享包，反过来不行。

## 数据流

```text
OpenWrt UCI ──严格解析──┐
Linux JSON ──严格解析───┼→ 配置 → 校验 → 编译
macOS 界面/JSON ─────────┘              │
                                        ↓
                          sing-box 配置 + 平台执行计划
                                        ↓
                    准备（Prepare）→ 启用（Activate）→ 健康检查（Healthy）→ 收尾（Finalize）
```

编译结果只取决于配置、状态目录和平台提供的 sing-box 入站部分。时间、测速结果、订阅状态和系统运行信息都不会混进配置或编译结果。

## 路由与 DNS

`route.detour` 表示一条节点路由经过另一条节点路由出去（链式代理）：

```text
应用流量 → 出口路由/节点 → 前置路由/节点 → 网络
```

校验会拒绝指向不存在、已停用或不是节点路由的前置，也会拒绝任何环。编译器为每条节点路由生成独立的出站 `steer-route-<id>`，所以同一个节点在不同路由里可以有不同的前置链，不需要全局的节点选择器。

每条启用的规则实际用到的「DNS 配置 + 路由」组合，会编译成一个独立的 DNS 通道：

- 走代理的路由，DNS 也走同一个出站，自动带上完整的前置链。
- 直连不设置 detour。
- 拒绝路由（配置里为兼容写作 `kind=block`）在流量规则和 DNS 规则里都生成 `action: reject`，不创建 block 出站。

普通规则和 DNS 规则共用能共用的匹配条件；目标 IP、网络类型、协议和端口只对流量生效，不用于 DNS。

## IPv6 内核直连

这是一个可选功能，由 `main.direct_bypass` 控制，取值 `off`（默认）、`static`、`dns`。思路是：如果按规则能提前确定某个 IPv6 连接一定走直连，就让内核直接转发，不进 sing-box。

共享编译器在完整的业务规则之前生成直连计划。平台通过 `BypassInboundTags` 声明哪些入口可以用。`compiler/bypass.go` 对每个条件判断三种结果：一定匹配、一定不匹配、不确定。

- 对域名条件取反时，要先确认映射里确实有这个域名，不能把「没查到」当成「不匹配」。
- 应用协议条件、缺失的 MAC 也不能当作「一定不匹配」的依据。
- 多个条件是「且」关系时，只要有一个一定不匹配，整条规则就一定不匹配。

一条直连规则能走内核，需要满足：它自己一定匹配，并且排在它前面的所有非直连规则都一定不匹配（前面的直连规则不影响结果）。默认规则放在最后，停用的规则不参与。

内核直连只生成不带出站的 `bypass` 动作，在不支持的阶段会被跳过；之后照常进行嗅探、解析和完整的规则匹配。TCP/UDP 53 不走内核直连。这个功能没有增加新的进程、DNS 缓存或 nftables 地址集合。`dns` 模式接受「域名映射可能有歧义」这一点，不保证结果一定和之后嗅探出的域名一致。实际效果由平台的原生检查和隔离网络测试验证。

## OpenWrt 数据面

流量主要由 sing-box 1.14 TUN 的 `auto_route`、`strict_route`、`auto_redirect` 和 `dns_mode: hijack` 接管，普通的 DNS 重定向也由它完成。还保持原目标地址进入 TUN 的 TCP/UDP 53 流量，编译器会在嗅探和内核直连之前显式加 `hijack-dns`，覆盖原生重定向没改写地址的情况。

Steer 自己只保留发往路由器本机地址的 DNS 例外：PREROUTING 处理局域网查询路由器本身的情况，OUTPUT/SNAT 处理路由器自己查询本地 DNS 的情况。其他 DNS 重定向都交给 sing-box。路由器自己的 UDP 123（NTP）明确走直连。

ping（ICMP echo）由共享编译器在普通代理规则之前按规则匹配：保留规则顺序、WireGuard 和拒绝，本该走代理的目标改走直连。源地址可路由的走内核直连；本机已经选用 TUN 地址作为源地址的请求强制走直连，重新选择物理网卡的源地址，否则 IPv6 按源地址选择的默认路由无法路由 TUN 的 ULA 地址。

「整台设备直连」的 MAC 规则（只按 MAC 匹配、没有其他条件、前面也没有冲突的非直连规则）由共享编译器挑出来，平台把它转成 TUN 原生的 `exclude_mac_address`，本机 DNS 规则也同步放行；不用自定义的标记绕过。其他源 MAC 条件由 sing-box 1.14 的 `source_mac_address` 在流量规则和 DNS 规则里原生匹配，不再有专门的 TProxy 或 MAC DNS 入口和策略路由。

TUN 名称、地址、路由表、优先级、标记、NFQUEUE 和兼容 DNS 端口都由平台管理，不出现在用户配置里。

OpenWrt 的 TUN 兜底策略路由优先级是 100000，排在 netifd 的 IPv6 源接口路由（90000）之后。这样被原生 MAC 排除、不带标记的 TCP 可以按委派前缀路由转发，被接管的流量仍然通过前面带标记的规则进入 TUN。Linux 通用平台用的是 32768。

## Linux 数据面

Linux 支持 systemd 主机本身以及它上面的虚拟机/Docker 的转发流量，不限制 `include_interface`。TUN 设置了 `dns_mode: hijack`，sing-box 负责普通的 TCP/UDP 53 重定向和 systemd-resolved 的网卡 DNS。进入 TUN 的 TCP/UDP 53 同样在嗅探和内核直连之前显式 `hijack-dns`。

Steer 的 PREROUTING/OUTPUT DNS 规则只处理发往本机地址的查询，并保留必要的 SNAT 和专用端口的访问保护，覆盖本机和局域网查询本地 DNS 的情况。ping 的处理和 OpenWrt 一样（这条规则不代表已经验证过 PMTUD）。源 MAC 条件靠原生的邻居表解析匹配。不修改 NetworkManager 的连接、`/etc/resolv.conf` 或 systemd-resolved 的配置片段，也不生成 MAC 策略路由。

`platform/linux` 自己决定 TUN、DNS、路由表、优先级、标记和 NFQUEUE。这些只保存在运行配置目录里的 `platform.json`，不进入用户配置，也没有用户可编辑的 `/etc/steer/platform.json`。

Geo 分类是共享的配置内容，两个平台都使用随包的 `/usr/share/steer/geodata-seed`。`internal/geodata` 按严格的清单解析分类，并在切换运行配置之前检查需要的 SRS 文件类型、大小和 SHA-256。

编译器为用到的 Geo 分类生成 sing-box 的远程规则集：随包的 SRS 作为 `initial_path`，Pages 上的同名 SRS 作为 `url`，每天更新一次。下载用显式的直连 HTTP 客户端，不受用户代理规则影响；`/var/lib/steer/cache.db` 由 sing-box 保存下载缓存。首次启动不需要联网，远程更新失败只记在 sing-box 日志里，不影响已经用本地数据启动成功的应用结果。

Steer 不设置全局的 `dns.strategy`，也不在 DNS 规则上设置查询级的 `strategy`，客户端的 A/AAAA 查询原样处理。SOCKS、HTTP、Mixed 本地入口收到的域名目标，在嗅探后先执行一次 `resolve`。这个动作不固定 DNS 服务器，而是交给 DNS 模块按用户规则选择对应的「DNS 配置 + 路由」，之后的流量规则再用解析结果匹配。目标本来就是 IP 时不会查询。DNS 服务器本身是域名时，解析它用的 `domain_resolver.strategy` 来自 `bootstrap.strategy`；路由默认的域名解析也用这个策略。schema 9 删掉了 `dns_profile.strategy`。

## macOS 数据面

macOS 由 LaunchDaemon 运行 sing-box 的 utun TUN，不用 pf，也不用 Network Extension。TUN 设置了 `dns_mode: hijack`。`_run` 作为父进程看护 sing-box，在 IPv4/IPv6 的 UDP/TCP DNS 入口可用后，把物理网络服务的系统 DNS 改成 `198.18.0.2` 和 `fdfe:dcba:9876::2`。

- 修改前按服务 UUID 持久保存原来的 DNS，并区分是自动获取还是手动填写。停止、sing-box 退出和卸载时恢复。
- control 后台服务每 5 秒检查一次：运行已经停止但留下了恢复记录，就执行恢复。这样被 SIGKILL 或重启后也能恢复。
- 用户之后自己改了某个服务的 DNS，Steer 就不再管它。
- 不修改 VPN 专用服务和搜索域。

TUN 使用默认公网路由加必要的排除。进入 TUN 的流量依次处理：① 目标端口 53 的 TCP/UDP 走 `hijack-dns`；② ping 按规则处理（直连/WireGuard/拒绝）；③ 私网地址直连；④ 嗅探；⑤ 解析；⑥ 用户规则。应用如果硬编码了链路本地或直连的 DNS，不保证会进 TUN。

网络轮询只接管新出现的物理网络服务，不读 Saved 配置、不生成运行配置，也不会自动应用。健康状态要求：DNS 地址路由到带 Steer 地址的本机 utun、DNS 入口可用、当前运行配置的系统 DNS 接管已完成。这不代表所有 VPN 的分域名解析或加密 DNS 都经过 Steer。

## 应用配置（Apply）

应用配置、写配置和订阅修改共用一把操作锁。应用是同步完成的：

1. 平台解析器严格解析配置；
2. 共享校验拒绝非法配置；
3. 共享编译器生成 sing-box 配置；
4. **准备**：检查 sing-box 能力、准备 Geo 数据、生成候选配置，并执行 `sing-box check` 和 `nft -c`；
5. **启用**：平台停掉旧服务，设置平台资源，把 `current` 指向候选配置，再启动；
6. **健康检查**：检查服务进程、TUN、nftables 和必要的监听端口；
7. **收尾**：删除其他运行配置。

第 4 步之前的失败不会改动正在运行的东西。`current` 在启动新进程之前就已更新，供 procd 读取。切换之后的失败会返回错误并保持出错时的状态；没有自动恢复、没有旧配置备份，也没有回滚接口。

停用走单独的 `Disable`：停止服务并清理运行资源（包括 nftables 规则）。OpenWrt 停用时只停 sing-box 而保留 procd 服务对象（以便下次 LuCI 提交能触发重新启用），所以 Steer 会在停用时自己删掉 `inet steer` 表，否则局域网 DNS 会被转发到已经没人监听的端口。

开机时：

- OpenWrt 由 procd 调用内部的 `_start`，完成校验、编译、准备和启用。
- Linux 由 systemd 调用内部的 `_run`，必要时准备启动用的运行配置，然后直接 `exec` 成 sing-box。
- macOS 由 LaunchDaemon 调用内部的 `_run`，必要时准备运行配置，然后把 sing-box 作为子进程启动并看护，负责 DNS 接管和恢复。

这些入口都不是公开命令，也不是第二套配置逻辑。macOS 图形界面和 LuCI、Linux 网页一样，只通过平台后台读写配置、触发公开的操作。

## 运行配置与状态

```text
/run/steer/generations/<id>/intent.json      用户配置
/run/steer/generations/<id>/sing-box.json    最终的 sing-box 配置
/run/steer/generations/<id>/platform.json    平台内部的资源计划
/run/steer/generations/<id>/firewall.nft     平台的 nftables 规则
/run/steer/current                            当前运行配置的链接
/run/steer/last-apply.json                    最近一次应用的结果
```

Linux 的权威配置是 `/etc/steer/config.json`，订阅快照和网页 token 在 `/var/lib/steer`。运行时只写 `/run/steer` 和 `/var/lib/steer`，不修改系统的 DNS 解析配置。

Linux 的 `status` 从 `current` 里实际的 `intent.json` 和 `sing-box.json` 得出当前运行的是什么（Active），最近一次应用的结果单独记录：

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

`last_apply.result` 永远不用来推断当前运行的配置。Linux 网页的 `pending_apply`（是否有待应用的修改）比较的是 Saved 和 Active 编译后的运行部分，而不是整个配置的摘要，所以订阅刷新了没被路由用到的节点时，不会提示需要应用。

OpenWrt 的 `status` 也一样，从 `current` 返回当前运行配置、配置摘要、运行摘要和健康状态，`last_apply` 单独记录。LuCI 内部的 `_state` 接口只返回不含凭据的启用状态、数量、摘要和校验结果；当前会话里未提交的修改和已提交的 UCI 分开读取，`pending_apply` 比较运行摘要，同样不会把订阅节点变化误报为运行变化。

配置是否合法由 `validate` 单独返回。各组件的细节留在平台健康检查和系统日志里，不加进跨平台的状态格式。

## 订阅与测速

共享的订阅逻辑只依赖一个很小的 `Store` 接口：替换某个订阅的节点，或删除一个节点。OpenWrt 的实现一次性提交一批 UCI 修改；JSON 的实现做带 revision 检查的原子写入。

- 订阅只修改 Saved 里的节点，不会自动应用。
- 合并时，上游删掉且没有路由引用的节点直接删除；仍被引用的保留并标记为过期，同时产生警告。
- 多个订阅一起更新时，一个失败不会影响其他订阅：成功的照常保存，失败的记录失败原因，最后统一报告错误。
- 三端的订阅状态都由 `subscription.Status` 生成：最近一次成功的快照和最近一次失败分开保存；失败摘要不包含 URL 和响应内容；过期节点会带上引用它的路由，用于阻止清理。界面上的更新结果也用这个格式，不返回含节点凭据的内部快照。

共享的 probe 负责 HTTP/TLS 测量、报告脱敏和保存。完整报告只给后台排错用；界面只拿到 `LatestProbeResult`，按「范围/对象 ID/测试类型」各保留一条，字段只有测试时间、成功与否、是否过期（后台计算）、一个主要指标的摘要、可选的数值 `metric_value` 和一个安全的错误摘要。

- 读取时每个键都完整返回，不做全局条数截断。
- 同一个键的写入用跨进程锁加原子替换，时间更早的结果不能覆盖更新的结果。
- Linux 的 HTTP 接口、OpenWrt 的 ubus/`_probe-results`、macOS 的 control 服务都提供同样含义的「批量获取最近结果」接口；发起测试的接口也返回同样的格式，测试失败时界面能马上显示刚保存的失败摘要。

Saved/Active 的身份、各阶段耗时、URL、尝试次数和完整错误只在后台报告和计算摘要时使用，不传给界面。界面负责本地化 `tested_at`、显示摘要，并按 `metric_value` 排序。连接指标单位是毫秒，下载指标单位是 Mbps；没有指标的结果不参与排序。

- 网络测试从各平台的 Saved 配置读取三个固定网址，直接用设备当前的网络访问，所以 Steer 没启用时也能测。它只说明目标当时能访问，不说明命中了哪个出口或 DNS。
- 节点和路由测试读取 Saved 配置，临时在回环地址上启动一个 sing-box。
- 测试结果不会进入配置，也不影响编译。

### OpenWrt netlink 故障恢复

procd 通过 `steer _supervise` 运行原版 sing-box。启动前临时把 `net.core.rmem_default` 调到至少 4 MiB，5 秒后改回原值；已经创建的 socket 会保留较大的接收缓冲区。调整失败只记警告，sing-box 照常运行。

看护进程每 5 秒检查一次 sing-box 拥有的 IPv4/IPv6 路由通知 socket。只有同时满足三个条件并持续一分钟才会重启 sing-box：单核 CPU 占用至少 80%、接收队列至少 128 KiB 且没有变化、已经有丢包。重启后有 10 分钟冷却期，记录在 `/run/steer/netlink-recovery.json`，看护进程自己重启也绕不过去。重启使用当前正在运行的配置，不会读取或应用尚未生效的 Saved 修改；重启原因写入系统日志。这是对上游问题的规避措施，不修改 sing-box 本身。

## ICMP

`compiler/icmp.go` 是三个平台共用的 ping 处理逻辑，不按操作系统、是否启用 WireGuard 或具体目标地址改变行为。它复用用户规则里的三层条件和顺序，在嗅探、解析和普通代理之前生成最终动作；网络类型、端口、应用协议和本地代理入口这些条件不适用于 ICMP，不会被当作三层条件。WireGuard 入口的访问控制总是排在这组规则之前。

平台只提供 TUN 网卡地址（保留主机位）和内核直连入口。编译器据此推导本机源地址的例外，避免已经选用 TUN 私有地址作为源地址的请求被送回物理网卡的内核路由。这个例外不绑定固定的 TUN 地址、WAN 网卡、域名或公网前缀。

实际发包测试覆盖 IPv4/IPv6 ping、本机和转发、TUN 源地址、目标丢包时不伪造回应、转发时的 TTL/Hop Limit 超时。macOS 的配置只在 Linux 命名空间里测过配置和 sing-box 的行为，不能代替 Mac 实机测试，也不代表所有 ICMP 类型或 PMTUD 都验证过。
