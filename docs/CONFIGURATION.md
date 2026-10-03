# 配置与使用

配置格式是 schema 9。每个平台只有一份配置，界面只编辑这一份：

- OpenWrt：`/etc/config/steer`（UCI），用 LuCI 编辑；
- Linux：`/etc/steer/config.json`（Canonical JSON），用网页界面编辑；
- macOS：`/Library/Application Support/Steer/config/config.json`（Canonical JSON），用 App 编辑。

下面的例子用 UCI 写法，JSON 里字段名相同。

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

三个测试网址都必须填，而且必须是 HTTPS，不能带用户名密码或 `#` 片段。没有默认值。

- `probe_direct`：直连测试的目标；
- `probe_proxy`：代理测试的目标，也是单独测试节点和路由时的默认目标；
- `speedtest_proxy`：下载测速的目标。

引导 DNS（Bootstrap）只支持 UDP/TCP，服务器必须写 IP。`strategy` 可选 `prefer_ipv4`、`prefer_ipv6`、`ipv4_only`、`ipv6_only`。引导 DNS 只用来解析 DNS 服务器这类基础设施的域名；直连的 UDP/TCP 引导 DNS 会发出明文 53 查询，但里面没有用户访问的域名。

## 节点、路由和前置代理

规则指向路由，不直接指向节点。路由有三种：`direct`（直连）、`block`（拒绝）、`single`（走某个节点）。

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

- `detour` 留空表示节点直接连接；填了就必须指向另一条启用的 single 路由。可以多层嵌套，但不能指向自己或绕成环。
- 直连和拒绝路由不能有 `detour`，也不能被当作别人的 `detour`。
- 拒绝路由为了兼容旧配置仍写作 `kind 'block'`，编译后是 sing-box 的 `action: reject`，不会生成已废弃的 block 出站。

支持的节点类型：`socks http shadowsocks vmess vless trojan hysteria shadowtls tuic hysteria2 anytls ssh naive tor`。具体字段看协议；未知字段、写错的字段和协议不支持的选项都会报错。三端都可以把节点导出成分享链接，格式和限制见[分享链接兼容性](SUBSCRIPTION_COMPATIBILITY.md)。

## DNS 配置

```uci
config dns_profile 'secure_dns'
	option protocol 'https'
	option server '1.1.1.1'
	option server_port '443'
	option tls_server_name 'one.one.one.one'
	option path '/dns-query'
```

- 协议支持 `udp tcp tls https quic h3`。
- 规则选了代理路由时，DNS 查询也走同一条路由和它的整条前置链。
- 本地 SOCKS/HTTP/Mixed 入口收到域名时，会先用 sing-box 的 `resolve` 按同一套 DNS 规则解析，再做路由。所以直连不会用引导 DNS 解析业务域名，代理也用规则选中的 DNS 配置。目标已经是 IP 的不解析。
- Steer 不设置全局的 `dns.strategy`，客户端自己发的 A/AAAA 查询原样处理。DNS 服务器写的是域名时，用引导 DNS 的 `strategy` 解析它。sing-box 1.14 废弃了 DNS 规则里的 `strategy`，所以 schema 9 也去掉了 `dns_profile.strategy`。
- 缓存容量、持久化和乐观缓存是全局设置。

**DNS 接管的范围**

- 各平台只接管进入各自接管路径的 TCP/UDP 53 端口查询。
- 应用自己发的 DoH、DoT、DoQ 是普通流量，53 端口接管认不出来，也改不了。所以界面和文档都不承诺「所有 DNS 都经过选定的 DNS 配置」。
- 诊断页的 DNS 检查只核对当前运行的配置里有没有预期的 sing-box/nftables 规则，不是抓包，不能证明没有泄漏。
- 为了网络稳定，Steer 不会把整个 TUN 或本地链路的流量都丢给 DNS 劫持。

**ping**

ICMP echo 按共享的三层规则处理，规则顺序和直连、WireGuard、拒绝都照常生效；命中普通代理路由的改走直连。Linux/OpenWrt 上来源地址可路由的交给内核转发，来源是本机 TUN 地址的由 sing-box 直连出口发出；macOS 全部经 TUN 转发。

## 规则

规则严格按配置里的顺序匹配，命中第一条就停。最后必须有且只有一条启用的默认规则，默认规则后面不能再有启用的普通规则。

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

可用的条件：

- `inbound`：本地 SOCKS、HTTP 或 Mixed 入口的 ID；
- `domain_match`：关键字，或 `full:`、`domain:`、`regexp:`、`geosite:` 开头；
- `ip_match`：CIDR 或 `geoip:`；
- `source_ip_cidr`、`source_mac_address`；
- `network`、`protocol`、`port`。

同一个字段的多个值是「或」，不同字段之间是「且」。目标 IP、网络、协议和端口只影响流量走哪条路由，不影响选哪个 DNS 配置。

`geosite:` 和 `geoip:` 必须是安装包 manifest 里确实存在的分类，`geosite:steam@cn` 这种带属性的写法也按完整名称检查。Apply 在停掉当前配置之前，先校验需要的 Geo 文件路径、大小和 SHA-256。sing-box 启动时用本地文件，之后每 24 小时在后台检查 `https://gsh20040816.github.io/steer/geodata/latest/`。远端更新失败不影响已有数据，错误只记在 sing-box 日志里。

## 本地入口

支持 `socks`、`http`、`mixed`，只能监听回环地址：

```uci
config local_proxy 'local'
	option protocol 'mixed'
	option listen '127.0.0.1'
	option listen_port '1090'
```

## 订阅

订阅只管节点：

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

**更新时机**

- 新建订阅的默认更新周期是 `6h`。`update_interval` 留空表示只能手动更新。
- 各平台每 15 分钟检查一次，只有从没更新过或者到期了才会下载。
- 用 `--id` 手动更新时不看周期。

**更新内容**

- URL 必须是 HTTP 或 HTTPS，可以是私网地址，可以重定向。
- 内容可以是逐行的分享链接，也可以是整段 Base64。
- 无效的节点跳过并计数。一个有效节点都没有时，这次更新算失败，保留上次的节点。
- 节点 ID 在多次更新之间保持不变，本地设置的启用状态也保留。
- 上游消失、又没有被路由引用的节点，这次更新直接删掉。被路由引用的保留下来，标为 `pinned_stale` 并给出警告，提醒尽快换掉引用。换掉之后，等下次更新自动删除，或者手动 `clean`。`clean` 不会顺带改路由。
- 订阅更新不会自动 Apply，也不会因为节点列表变了就生成新的运行配置。三端都会提示新增、现有、过期、跳过的数量，并说明正在运行的配置没变。

**状态字段**

三端用同一组字段：`never_fetched`、`last_success`（可能为空）、`last_failure`、`node_count`、`current`、`added`、`skipped`，以及每个节点的 `stale`。失败只记时间和去掉敏感信息的摘要，不会覆盖上次成功的时间和节点。`stale.referenced_by` 列出还在引用它的路由。停用的订阅不能更新。

## Apply、状态和测试

```sh
steer validate
steer apply
steer health --timeout 10s
steer status
```

- `validate` 只做解码和配置校验。
- `apply` 还会检查 sing-box 能力、准备 Geo 数据、跑 sing-box/nftables 自带的配置检查、切换配置、做本地健康检查。
- `status` 只返回当前运行配置的身份（generation、Intent 和运行时的 digest）、`healthy`，以及可选的 `last_apply`。草稿、已保存配置和校验结果都不在里面。

网络测试读取已保存配置里的三个网址，直接用设备当前的网络访问，Steer 没启用也能测：

```sh
steer probe --kind direct
steer probe --kind proxy
steer probe --kind speedtest
```

节点和路由测试读取已保存的配置，在回环地址上临时起一个 sing-box，不影响正在运行的配置：

```sh
steer probe --kind speedtest --node <node-id>
steer probe --kind speedtest --node <node-id> --download
steer probe --kind speedtest --route <route-id>
steer probe --kind speedtest --route <route-id> --download
```

**测试结果**

- 连接测试在内部记录 TCP、TLS、首字节、HTTP 状态和重试次数；下载测试记录字节数、耗时和速率。完整报告只留给后台排错，并去掉凭据、URL 路径和参数、进程信息。
- 每个「网络 / 节点 / 路由 + 测试类型」只保留最近一次结果。界面拿到的是后台算好的摘要：`scope/object_id/kind/tested_at/ok/stale/summary/error_summary`。界面只负责本地化时间和显示，不自己判断是否过期，也不自己算延迟或速率。
- 已保存配置或网络环境变了，旧结果标为过期。测试失败也会立即返回刚保存的结果。
- 网络测试成功只说明网址当时能访问，不说明走了哪个出口、用了哪个 DNS，也不说明没有 DNS 泄漏。节点和路由测试只验证那条临时链路，不说明你的规则会选中它。

## 版本升级

0.8.0 起只接受 schema 9。各平台都没有旧格式的迁移命令或安装钩子，旧配置要在升级前自己改好，否则校验和 Apply 会报错。

## 整设备直连（按 MAC）

Linux/OpenWrt 上，如果一条规则只有 `source_mac_address` 条件、并且指向直连，它会被编译成 TUN 的 `exclude_mac_address`：这台设备的 IPv4/IPv6 TCP、UDP、ICMP 全部不进 TUN，平台的本机 DNS 拦截也跳过它。这台设备发给外部 DNS 的查询不再经过 Steer，发给路由器自己的 DNS 查询照常由那个 DNS 服务处理。这样做还能在 Docker 对进入 TUN 的 UDP 做 MASQUERADE 之前认出设备，不会因为来源被改写而误走默认代理。

只有在这条规则之前没有可能命中同一台设备的非直连规则时，才会这样处理。其他 MAC 的规则、或者明确限定了本地代理入口的规则不算冲突。如果 MAC 规则还带了域名、IP、端口、协议、网络或入口条件，就按普通规则处理。需要整设备直连时，把纯 MAC 的直连规则放到可能冲突的代理/拒绝规则前面。

macOS 不生成这个选项。它和下面的 `direct_bypass` 互不相关。

## IPv6 内核直连（0.11.0）

`main.direct_bypass` 是可选字段，不写或写 `off` 就和以前一样。OpenWrt 写 `option direct_bypass 'dns'`，JSON 写 `"direct_bypass": "dns"`，LuCI 和 Linux 网页的基础设置里也有这个选项。macOS 不支持，开启会报错。

| 值 | 效果 |
|---|---|
| `off` | 先嗅探，再走完整的规则匹配。 |
| `static` | 根据 IP、来源网段、端口、传输层协议和已知的来源 MAC，提前判断一定走直连的流量。域名条件当作未知。 |
| `dns` | 在 `static` 基础上，用 DNS 反查得到的域名参与判断；查不到域名的仍当作未知。 |

**适用范围**

- 只对 Linux/OpenWrt 经 `auto_redirect` 的 IPv6 流量生效。规则里明确写的 TCP/UDP 条件照常按实际报文匹配。
- TCP/UDP 53 仍由 DNS 接管；IPv4 和显式的 HTTP/SOCKS/Mixed 入口不受影响。
- ICMP 的旁路是另一套机制，和这个选项无关。
- DNS 传输、订阅和测试的流量不会因为这个选项改走内核。

**判断规则**

- 前面的代理/拒绝规则必须能被确定排除。没有域名不等于域名规则不匹配。
- 前面的未知规则如果同样指向直连，不影响后面已经确定的直连。
- 例子：先有一条按域名走代理的规则，后面是 `geoip:cn → 直连`。没有域名时无法排除前一条，只能退回嗅探。
- 应用协议条件在预判阶段是未知的；但如果同一条规则里的端口、网络或来源网段明确不匹配，整条规则可以排除。
- 来源 MAC 匹配失败可能只是邻居表里没有记录，所以不能用它证明前面的 MAC 代理/拒绝规则不匹配。

**注意**

- `dns` 模式接受共享 IP、CNAME、缓存带来的域名歧义，结果不一定和之后的 SNI/Host 一致。
- 无法提前确定直连时，仍然先嗅探/解析，再从第一条规则开始完整匹配。
- 普通的直连出站不保证保留客户端的公网 IPv6。真正的内核直连还需要正常的 IPv6 转发、回程路由，并且中间没有改写来源地址。
