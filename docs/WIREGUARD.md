# WireGuard（macOS）

从 0.11.1 起，macOS Steer 可以通过 sing-box WireGuard endpoint 管理双向隧道。无需运行官方 WireGuard App，也不需要额外的 Network Extension。当前发行版的完整管理入口和 Endpoint 刷新仅在 macOS 提供；Linux/OpenWrt 对启用的托管隧道明确报平台不支持。

## 配置与使用

1. 在 **WireGuard → 导入 .conf** 粘贴配置或选择文件，检查导入预览。
2. 默认创建一条隧道、一个 WireGuard 出口，以及一条位于 Default 之前的 AllowedIPs 规则。取消自动规则后，可以自行创建域名/IP/端口规则引用该出口。
3. 保存并应用。运行期间不要再同时启用使用相同密钥的官方 WG 隧道，以免同一个 Peer 在两个客户端之间漫游。

对象关系为 `wireguard_tunnels → routes(kind=wireguard, tunnel=<ID>) → rules(route=<ID>)`。一条隧道拥有私钥、本地地址、MTU、监听端口和多个 Peer；Peer 拥有 Endpoint、公钥、可选预共享密钥、AllowedIPs、Keepalive。底层使用 `system: false`，不额外创建 WG 系统网卡。

`rules[].allowed_ips: true` 表示目标 IP 匹配所选出口对应隧道的所有 Peer AllowedIPs。只保存引用，编译时展开，不能同时填写 `ip_match`，也不能用于 Default。普通规则仍按顺序匹配；不同隧道的重叠网段给出告警，同一隧道不同 Peer 的完全相同前缀拒绝保存。

`0.0.0.0/0` 或 `::/0` 会匹配相应地址族的剩余流量，导入预览和校验会明确提示。AllowedIPs 仍保留在 WireGuard Peer 配置中，用于选 Peer 和校验收到数据包的源地址，不会因生成分流规则而删除。

启用 WG 后，macOS 私网 Direct 从用户规则前移到普通规则之后、Default 之前，规则页显示该兜底位置。明确的 WG 网段也加入 TUN 的 route_address。与物理 LAN 完全重叠、链路本地或系统保留网段需要另行规划，不能把更改 AllowedIPs 当成解决所有接口作用域/路由冲突的办法。

## 远端访问本机

隧道编辑器中的“远端访问本机”允许指定：远端源 CIDR、TCP/UDP、WG 目标端口、本机回环地址和服务端口。例如允许 `10.20.0.1/32 → TCP 22 → 127.0.0.1:22`。未列出的 WG 入站连接全部拒绝，不能落入普通代理分流或默认出口。

本地服务必须监听对应回环地址或通配地址。IPv4/IPv6 可分别转发到 `127.0.0.1` / `::1`，也可显式选择另一地址族的回环目标。当前仅开放本机服务，不提供任意 LAN 转发。本机应用通常看到回环源地址，原始远端源地址由 Steer 入站规则检查。

sing-box 会把本地地址前缀内的目标映射到回环，因此本地地址只接受 `/32` 或 `/128`。导入常见的 `/24`、`/64` 配置时保留地址并转换为主机前缀，预览中展示提示；远端网络继续由 AllowedIPs 表达。

## Endpoint 域名与 IPv6 更新

Endpoint 可保留域名，每条隧道可以选择继承 Bootstrap、优先 IPv4/IPv6 或仅 IPv4/IPv6。引导解析必须在 WG 未连接时可达；WG Endpoint 域名固定交给 Bootstrap，不使用经 WG 才能访问的 DNS。

macOS supervisor 启动约 10 秒后检查，随后每 60 秒检查 Endpoint 地址。查询通过 Steer DNS 入口进行，Endpoint 域名禁用 DNS 缓存。记录仍包含当前地址时继续使用，避免多地址 DNS 排序变化造成反复重连；解析失败保留原地址并在下一轮重试。

解析地址存放在独立运行配置，原始配置和 generation 中的域名保持不变。地址变化通过 SIGHUP 重载 sing-box，**会重建核心，现有连接可能中断**；首次无缓存启动后固定解析地址也可能重载一次。缓存的最近地址可供下次启动使用，随后重新检查 DNS；它不是握手成功证明。

WireGuard 页展示的是 Endpoint 解析结果和错误，不把它标成握手或服务可用状态。当前没有单 Peer 的无损热更新、握手统计或自动探测后回退到其他候选地址。

## 导入与安全边界

导入支持 Interface 的 PrivateKey、Address、ListenPort、MTU、DNS 和 Peer 的 PublicKey、PresharedKey、Endpoint、AllowedIPs、PersistentKeepalive。`DNS=` 仅作为导入提示，不自动修改 DNS Profile 或系统 DNS；内网域名仍需配置相应 DNS 规则。PostUp/PostDown、Table、SaveConfig 和其他未实现字段拒绝导入，不执行脚本或静默丢弃。

私钥和预共享密钥只进入受保护配置；生成的运行配置和地址缓存为 0600，临时目录为 0700。解析状态不含密钥。关闭 Steer 会清理当前临时运行配置；用户保存的配置和最近地址缓存保留。

WG 接入不改变现有系统 DNS 接管策略，也不承诺透明处理任意 IP 协议、保留应用原始源 IP、站点互联或覆盖所有绕过 TUN 的 DNS。

## 验证

共享 Go 测试覆盖导入、字段/引用校验、AllowedIPs 更新、分流顺序、入站默认拒绝、地址缓存和失败重试；Swift 测试覆盖导入事务、自动引用和 Default 位置。

真实双端测试使用发布固定的 sing-box 1.14.1，在本机建立两个用户态 WG endpoint，验证 TCP/UDP 往返、经 WG 查询内网 DNS 后访问服务，以及未开放服务拒绝，不创建 TUN、不修改主机路由。运行：

```sh
cd go
STEER_WG_SING_BOX=/absolute/path/to/sing-box go test ./internal/wireguard -run TestNativeWireGuardRemoteAccess -count=1 -v
```

同一测试也作为 macOS DMG 发布门执行。
