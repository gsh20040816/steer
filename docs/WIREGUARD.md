# WireGuard（macOS）

从 0.11.1 起，macOS 版 Steer 可以直接用 sing-box 的 WireGuard endpoint 建立双向隧道，不需要官方 WireGuard App，也不需要 Network Extension。目前只有 macOS 支持完整的隧道管理和 Endpoint 地址刷新；Linux 和 OpenWrt 遇到启用的隧道会直接报「平台不支持」。

## 使用

1. 打开 **WireGuard → 导入 .conf**，粘贴配置或选择文件，看一下导入预览。
2. 默认会创建一条隧道、一个 WireGuard 出口，以及一条放在默认规则前面的 AllowedIPs 规则。不想要这条自动规则可以取消，自己写按域名、IP 或端口匹配的规则指向这个出口。
3. 保存并应用。运行期间不要同时开着用同一套密钥的官方 WireGuard 隧道，否则同一个 Peer 会在两个客户端之间来回切换。

对象之间的关系是 `wireguard_tunnels → routes(kind=wireguard, tunnel=<ID>) → rules(route=<ID>)`。一条隧道有私钥、本地地址、MTU、监听端口和若干 Peer；每个 Peer 有 Endpoint、公钥、可选的预共享密钥、AllowedIPs 和 Keepalive。底层设置了 `system: false`，不会多出一块系统网卡。

**AllowedIPs 规则**

- 规则里写 `allowed_ips: true`，表示「目标 IP 落在这个出口所属隧道的任一 Peer 的 AllowedIPs 里」。配置里只存这个引用，编译时才展开成具体网段。
- 它不能和 `ip_match` 同时用，也不能用在默认规则上。
- 不同隧道的网段重叠会给出警告；同一条隧道里两个 Peer 写了完全相同的网段，不允许保存。
- 写了 `0.0.0.0/0` 或 `::/0`，就会匹配该地址族剩下的所有流量，导入预览和校验都会提醒。
- 生成规则不会改动 Peer 自己的 AllowedIPs，它们仍然用来选择 Peer 和检查收到的包的来源地址。

**和私网直连的关系**

启用 WireGuard 后，macOS 的「私网走直连」从用户规则前面挪到普通规则之后、默认规则之前，规则页会显示这个位置。明确的 WireGuard 网段也会加进 TUN 的 `route_address`。和物理局域网完全重叠的网段、链路本地或系统保留网段需要你自己规划，改 AllowedIPs 解决不了所有路由冲突。

## 让对端访问本机服务

隧道编辑器里的「远端访问本机」可以设置：对端来源网段、TCP/UDP、隧道里的目标端口、本机回环地址和服务端口。比如 `10.20.0.1/32 → TCP 22 → 127.0.0.1:22`。没有列出来的入站连接一律拒绝，不会掉进普通分流或默认出口。

- 本机服务要监听对应的回环地址或通配地址。IPv4 和 IPv6 可以分别转到 `127.0.0.1` 和 `::1`，也可以指定转到另一个地址族的回环地址。
- 目前只能访问本机服务，不能转发到局域网其他机器。
- 本机应用看到的来源通常是回环地址，对端的真实来源由 Steer 的入站规则检查。
- sing-box 会把本地地址网段内的目标都映射到回环，所以本地地址只能是 `/32` 或 `/128`。导入常见的 `/24`、`/64` 配置时，地址保留、前缀改成单个主机，预览里会提示；对端网段仍然写在 AllowedIPs 里。

## Endpoint 域名和地址刷新

Endpoint 可以写域名。每条隧道可以选择跟随引导 DNS，或者优先 IPv4/IPv6、只用 IPv4/IPv6。Endpoint 域名固定用引导 DNS 解析，不会用必须经过隧道才能访问的 DNS，所以引导 DNS 要在隧道没连上时也能用。

- macOS 的看护进程启动约 10 秒后检查一次，之后每 60 秒检查一次 Endpoint 地址。查询走 Steer 的 DNS 入口，并且不使用 DNS 缓存。
- 如果 DNS 返回的地址里还包含当前在用的地址，就继续用，避免多地址的顺序变化导致反复重连。解析失败时保留原地址，下一轮再试。
- 解析出的地址写在单独的运行配置里，用户配置和生成的配置里仍然是域名。
- 地址变化时通过 SIGHUP 让 sing-box 重新加载。**这会重建核心，现有连接可能会断。** 第一次启动没有缓存时，固定下解析地址也可能触发一次重载。
- 最近一次的地址会缓存下来供下次启动使用，启动后再重新查 DNS。缓存的地址不代表握手成功过。

WireGuard 页显示的是 Endpoint 的解析结果和错误，不代表握手成功或服务可用。目前没有单个 Peer 的无中断更新、握手统计，也不会在探测失败后自动换用其他地址。

## 导入范围和安全

能导入的字段：Interface 的 PrivateKey、Address、ListenPort、MTU、DNS，Peer 的 PublicKey、PresharedKey、Endpoint、AllowedIPs、PersistentKeepalive。

- `DNS=` 只作为导入时的提示，不会改 DNS 配置或系统 DNS。内网域名需要你自己加 DNS 规则。
- PostUp/PostDown、Table、SaveConfig 和其他不支持的字段会让导入失败，不会执行脚本，也不会悄悄丢掉。
- 私钥和预共享密钥只存在受保护的配置文件里。生成的运行配置和地址缓存权限是 0600，临时目录是 0700，解析状态里没有密钥。
- 停用 Steer 会删掉临时运行配置，用户配置和地址缓存保留。

WireGuard 不改变现有的系统 DNS 接管方式，也不保证：透明处理任意 IP 协议、保留应用的原始来源 IP、站点互联、接管所有绕过 TUN 的 DNS。

## 测试

共享 Go 测试覆盖导入、字段和引用校验、AllowedIPs 更新、规则顺序、入站默认拒绝、地址缓存和失败重试；Swift 测试覆盖导入流程、自动规则和默认规则的位置。

还有一个真实的双端测试：用发布时固定的 sing-box 1.14.1 在本机起两个用户态 WireGuard endpoint，验证 TCP/UDP 往返、通过隧道查询内网 DNS 后访问服务，以及没开放的服务被拒绝。它不创建 TUN，也不改主机路由：

```sh
cd go
STEER_WG_SING_BOX=/absolute/path/to/sing-box go test ./internal/wireguard -run TestNativeWireGuardRemoteAccess -count=1 -v
```

macOS DMG 发布前也会跑这个测试。
