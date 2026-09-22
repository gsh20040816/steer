# 配置与使用

当前公开配置是 schema 9。OpenWrt 的 `/etc/config/steer` 是唯一配置真相，LuCI 只编辑这份 UCI；Linux 的 `/etc/steer/config.json` 与 macOS 的 `/Library/Application Support/Steer/config/config.json` 是严格 Canonical JSON 真相。Linux Web 和 macOS GUI 都只编辑各自平台的这份配置，不维护第二套前端 schema。Geo category 由包内 manifest 精确校验，不再配置 DAT 路径。

## 基本配置

```uci
config steer 'main'
	option schema_version '9'
	option enabled '1'
	option log_level 'warn'
	option dns_cache_capacity '4096'
	option dns_cache_persist '1'
	option dns_optimistic_cache '1'
	option probe_direct 'https://www.baidu.com/'
	option probe_proxy 'https://www.google.com/generate_204'
	option speedtest_proxy 'https://speed.cloudflare.com/__down?bytes=1000000'

config bootstrap 'bootstrap'
	option protocol 'udp'
	option server '1.1.1.1'
	option server_port '53'
	option strategy 'prefer_ipv4'
```

三个测试地址都必须是没有凭据和 fragment 的单个 HTTPS URL：

- `probe_direct`：使用设备当前网络环境访问的直连测试目标；
- `probe_proxy`：使用设备当前网络环境访问的代理测试目标，以及裸节点和路由链临时测试的默认目标；
- `speedtest_proxy`：完整下载测速。

后端不会补默认 URL。Bootstrap 只支持 UDP/TCP，服务器必须是 IP 字面量，策略为 `prefer_ipv4`、`prefer_ipv6`、`ipv4_only` 或 `ipv6_only`。Bootstrap 只解析 DNS 上游等基础设施主机名；Direct UDP/TCP Bootstrap 可能产生明文 53，但其中不是原始业务查询域名。

## 节点、路由和前置代理

规则引用 Route，不直接引用 Node。Route 可以是 `direct`、`block` 或 `single`：

```uci
config node 'front_node'
	option name 'Front'
	option type 'socks'
	option server '192.0.2.10'
	option server_port '1080'

config node 'exit_node'
	option name 'Exit'
	option type 'vless'
	option server 'proxy.example'
	option server_port '443'
	option uuid '00000000-0000-4000-8000-000000000001'
	option tls_server_name 'proxy.example'

config route 'direct'
	option kind 'direct'

config route 'front'
	option kind 'single'
	option node 'front_node'

config route 'exit'
	option kind 'single'
	option node 'exit_node'
	option detour 'front'

config route 'block'
	option kind 'block'
```

`detour` 可留空，表示节点直接拨号；非空时必须引用另一条启用的 single Route。链可以多级，但不能自环或间接成环。Direct/Reject 不能携带或充当前置 Route。为兼容已有配置，Reject 仍写为 `kind 'block'`；编译后为 sing-box route/DNS `action: reject`，不生成已废弃的 block outbound。后端是唯一语义裁决者，Apply 会拒绝完整非法路径。

支持的节点类型：`socks http shadowsocks vmess vless trojan hysteria shadowtls tuic hysteria2 anytls ssh naive tor`。具体字段由协议决定；未知字段、错误字段形态和该协议不支持的选项都会明确失败。三端节点列表都可以把可表示的当前节点导出为分享链接；分享链接的输入/输出格式、参数闭环和 manual-only 类型见[分享链接兼容矩阵](SUBSCRIPTION_COMPATIBILITY.md)。

## DNS Profile

```uci
config dns_profile 'secure_dns'
	option protocol 'https'
	option server '1.1.1.1'
	option server_port '443'
	option tls_server_name 'one.one.one.one'
	option path '/dns-query'
```

协议支持 `udp tcp tls https quic h3`。规则选择代理 Route 时，DNS transport 使用同一 Route 及其完整前置链。本地 SOCKS、HTTP 或 Mixed 入口收到域名目标后，会在业务路由前通过 sing-box 原生 `resolve` action 复用同一组 DNS rules，因此按匹配规则选择 DNS Profile；Direct 不会再用 Bootstrap 解析业务域名，Proxy 也使用所选 Profile 的解析结果。已经是 IP 的目标不会触发该查询。Steer 不设置全局 `dns.strategy`，普通客户端明确发出的 A/AAAA 查询保持透明；DNS server 使用域名时，其 `domain_resolver.strategy` 来自独立的 `bootstrap.strategy`。sing-box 1.14 已废弃 DNS rule action 的 query-level `strategy`，schema 9 删除了原 `dns_profile.strategy`，Steer 不再生成该字段。缓存容量、持久化与乐观缓存是全局设置。

平台 capture 只接管进入各自捕获路径的 TCP/UDP 目标端口 53；ICMP echo（ping）遵循共享 L3 策略，保留适用规则顺序和 Direct/WireGuard/Block，普通代理目标回退 Direct。Linux/OpenWrt 对可路由源地址执行内核 bypass，本机 TUN 源地址改用 Direct 转发；macOS 通过 TUN 完成真实转发。应用自带 DoH、DoT、DoQ 是普通业务流量，port-53 capture 本身无法识别或重定向；除非另有经过验证的阻断/重定向策略，否则 UI 和文档都不承诺“全部 DNS 必然经过所选 Profile”。Diagnostics 的 DNS 检查只核对已发布 Active generation 中的预期 sing-box/nftables 配置，不是流量抓包，也不证明零泄漏。为保持网络稳定，Steer 不把整个 TUN 或本地链路流量无差别送入 DNS hijack。

## 规则

规则严格按 UCI 顺序 first-match。最后必须恰好有一条启用的 Default；Default 之后不能再有启用普通规则。

```uci
config rule 'service'
	option name 'Example service'
	option dns_profile 'secure_dns'
	option route 'exit'
	list domain_match 'domain:example.com'
	list domain_match 'geosite:category-example'
	list network 'tcp'
	list port '443'

config rule 'default'
	option default '1'
	option dns_profile 'secure_dns'
	option route 'direct'
```

条件包括：

- `inbound`：本地 SOCKS、HTTP 或 Mixed 入口 ID；
- `domain_match`：普通关键字、`full:`、`domain:`、`regexp:`、`geosite:`；
- `ip_match`：CIDR 或 `geoip:`；
- `source_ip_cidr`、`source_mac_address`；
- `network`、`protocol`、`port`。

同一字段多值为 OR，不同非空字段为 AND。目标 IP、网络、协议和端口只参与业务路由，不参与 DNS 选择。

`geosite:` 与 `geoip:` 引用必须精确存在于包内 manifest；`geosite:steam@cn` 之类属性 selector 也按完整名称校验。Apply 在停止当前 generation 前验证所需 seed 的路径、大小和 SHA-256。sing-box 随后使用本地 `initial_path` 启动，并每 24 小时后台检查 `https://gsh20040816.github.io/steer/geodata/latest/`。远端失败不会把有效 seed/cache 伪装成失败，错误保留在 sing-box 日志中。

## 本地入口和订阅

本地入口支持 `socks`、`http` 与 `mixed`，监听地址必须是 loopback：

```uci
config local_proxy 'local'
	option protocol 'mixed'
	option listen '127.0.0.1'
	option listen_port '1090'
```

订阅只管理节点：

```uci
config subscription 'public'
	option enabled '1'
	option name 'Public'
	option url 'https://example.com/subscription'
	option update_interval '6h'
```

```sh
steer subscription update --id public
steer subscription status
steer subscription clean --id public --node <node-id>
```

三端新增订阅的默认更新周期统一为 `6h`。`update_interval` 留空时订阅仅允许手动更新；平台每 15 分钟运行一次轻量调度器，只有非空周期首次抓取或已到期时才下载。带 `--id` 的显式更新属于手动操作，始终忽略周期限制。

URL 必须是可访问的 HTTP 或 HTTPS 地址，允许私网地址和正常重定向。订阅内容可为逐行标准代理 URI 或整段 Base64 URI 列表。单条无效节点会被跳过并计数；如果没有任何有效节点，更新失败并保留上次成功节点库。非空更新使用稳定 ID，保留本地启用状态；上游消失且未被 Route 引用的节点在本次更新中自动删除，仍被 Route 引用的节点才会保留并标为 `pinned_stale`，同时产生提醒尽快解除引用的 warning。解除引用后可等待下次更新自动删除，或显式 clean；cleanup 不级联改写 Route。订阅提交节点后不自动 Apply、不创建仅由库存变化触发的 generation；三端提示同时展示 added/current/stale/skipped，并明确当前 Active 配置未改变。

三端共用同一状态契约：`never_fetched`、可空的 `last_success`、独立的 `last_failure`、`node_count`、`current`、`added`、`skipped` 和逐节点 `stale`。失败只写入时间与脱敏摘要，不覆盖上次成功时间和节点库；`stale.referenced_by` 列出阻止清理的 Route。已停用订阅不能 Update。

## Apply、状态和测试

```sh
steer validate
steer apply
steer health --timeout 10s
steer status
```

`validate` 只做严格解码和共享语义校验。`apply` 还会执行能力检查、Geo 准备、sing-box/nftables 原生检查、切换与本地健康检查。`status` 只包含 Active generation/Intent/Runtime digest、`healthy` 和可选 `last_apply`；Draft、Saved、配置合法性和组件明细不会混入 Active 状态对象。

概览测试读取 Saved 配置中的三个 URL，并直接使用设备当前网络环境；Steer 未启用或没有 Active generation 时仍可运行。三端按测试种类持久化最近一次结果，并在 Saved 或测试时网络环境变化后标记为过期。`saved_digest`、`active_generation`、`active_digest` 等身份只用于后端判断，不进入普通测试结果 UI：

```sh
steer probe --kind direct
steer probe --kind proxy
steer probe --kind speedtest
```

裸节点和路由链测试读取磁盘 UCI并启动临时环回 sing-box，不切换当前运行态：

```sh
steer probe --kind speedtest --node <node-id>
steer probe --kind speedtest --node <node-id> --download
steer probe --kind speedtest --route <route-id>
steer probe --kind speedtest --route <route-id> --download
```

连接测试内部记录 TCP、TLS、首字节、HTTP 状态和尝试次数；下载测试内部记录字节、耗时和速率。平台 state 按 overview/node/route 与测试 kind 各保留一个原始报告，但普通控制面读取的是后端生成的 `LatestProbeResult`：`scope/object_id/kind/tested_at/ok/stale/summary/error_summary`。读取不按全局条数截断，失败动作也会返回或重新读取刚持久化的 DTO。普通 UI 不展示原始报告或历史列表，不比较 Saved/Active digest，也不自行计算 stale、延迟、吞吐率或错误分类；它只本地化时间并在测试入口展示后端摘要。完整阶段数据仅供受控排错使用，并继续去除 credentials、URL path/query values 与进程诊断。概览请求从 Saved 配置读取 URL，直接使用设备当前网络环境访问，不要求 Steer 已启用；成功只证明 URL 当时可达，不证明具体 outbound、DNS resolver 或 DNS 无泄漏。节点/路由测试只验证隔离临时链路，不证明当前规则选择了该链路。

## 版本与升级

0.8.0 及更高版本只接受 schema 9。Linux、OpenWrt 和发行版包均不再提供旧 schema 迁移命令或安装 hook；旧版本配置必须在升级前完成转换，否则 Validate/Apply 会明确失败。`bootstrap.strategy` 只服务内部域名解析，DNS Profile 不再包含客户端地址族 strategy。

## IPv6 内核直连（0.11.0）

`main.direct_bypass` 是 schema 9 的可选字段，省略或 `off` 保持既有行为。
LuCI/Linux 基础设置提供同一选项；OpenWrt 对应 `option direct_bypass 'dns'`，
Canonical JSON 对应 `"direct_bypass": "dns"`。

| 值 | 行为 |
|---|---|
| `off` | 先 sniff，再执行完整业务规则。 |
| `static` | 根据 IP、源 CIDR、端口、传输层协议和已确认的源 MAC 提前证明 Direct。域名条件保持未知。 |
| `dns` | 在 static 基础上，接受 DNS reverse mapping 域名参与提前判断；缺失域名仍是未知。 |

仅 OpenWrt/Linux auto_redirect 的 IPv6 流量参与新旁路，不附加传输层协议限制；
规则显式指定的 TCP/UDP 条件仍按实际报文匹配。TCP/UDP 53 继续由 DNS 接管，
IPv4 和显式 HTTP/SOCKS/Mixed 入口保持原路径。macOS 拒绝启用该选项。
已配置的 ICMP 旁路独立于此选项。

前置 Proxy/Reject 规则必须能够被排除；缺少域名不能视为域名规则不匹配。
前面的未知规则如果同样指向 Direct，则不阻止后面已经确定的 Direct。
例如前置域名 Proxy 后的 `geoip:cn → Direct`，在无域名时必须回退 sniff。
应用协议规则在预匹配阶段未知，但同一规则内明确不匹配的端口、网络或源网段可排除整条规则。
源 MAC 匹配失败可能是邻居信息缺失，因此不会被用于证明前置 MAC Proxy/Reject 不匹配。

DNS 辅助旁路接受共享 IP、CNAME、缓存等带来的域名歧义；它不保证与稍后 SNI/Host 相同。
不能提前证明 Direct 时，仍先 sniff/resolve，再从第一条完整业务规则开始判断。
普通 Direct outbound 不能保证保留客户端公网 IPv6；真正 bypass 还需要正常 IPv6 转发、回程路由且无源地址改写。
DNS transport、订阅与探测不会因为该选项改为内核旁路。
