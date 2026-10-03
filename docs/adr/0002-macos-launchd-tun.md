# ADR 0002：macOS 用 LaunchDaemon + sing-box TUN

## 状态

已采纳。这是 macOS 唯一支持的流量处理方式。

## 背景

项目不打算付费加入 Apple Developer Program，所以用不了 Network Extension。sing-box 本身就支持 macOS 的 TUN 和 `auto_route`，Steer 没必要自己实现。SwiftUI 图形界面只负责配置和运维，流量交给 root 运行的 LaunchDaemon。

## 决定

用普通的 sing-box 可执行文件，由 root LaunchDaemon 启动：

```text
steer-macos apply
        ↓
launchctl bootstrap
        ↓
steer-macos _run
        ↓
看护 sing-box，接管系统 DNS
        ↓
Darwin utun + auto_route
```

TUN 的设置：

- 不设 `interface_name`，由系统自动分配 utun 设备；
- 开启 `auto_route`，`stack=system`，设置 MTU 和地址；
- `route_address` 只有默认路由（启用 WireGuard 时再加上它的网段），不额外加 RFC1918、CGNAT、IPv6 ULA 这些私网路由；
- `route_exclude_address` 排除回环、链路本地、组播、文档地址和其他保留地址；
- 不用 Linux 才有的 `auto_redirect`，也不用 iproute2 mark、nftables 或 pf。

DNS 全部由 sing-box 的 DNS 模块处理。TUN 设置 `dns_mode=hijack`，不写 `dns_address`，由 sing-box 自己用 `198.18.0.2` 和 `fdfe:dcba:9876::2` 接收 DNS 查询。macOS 的命令行版 sing-box 不会修改系统 DNS，所以 `_run` 在确认 DNS 可用后，按网络服务的 UUID 记下原来的 DNS 并改成上面两个地址，退出时再改回去。进程崩溃留下的记录由常驻的 control 服务负责恢复。恢复时只处理 DNS 仍是 Steer 写入的那些服务，用户后来自己改过的不动；VPN 的网络服务和搜索域也不动。

进入 TUN 的流量按这个顺序处理：

1. `inbound=steer-tun`、TCP/UDP、目标端口 53 → `hijack-dns`；
2. ICMP 走专门的处理；
3. 私网目标 → 直连；
4. 协议嗅探，然后是用户规则。

第 1 条只管已经进入 TUN 的查询，管不到走直连或按网卡绑定的路由绕开 TUN 的查询，所以还需要接管系统 DNS 来覆盖默认的解析路径。网络变化时只重新处理系统 DNS，不会重新生成配置，也不会悄悄应用未保存的修改。

## 生命周期

`steer-macos apply` 是同步完成的：

```text
校验 → 编译 → sing-box check → 准备新配置目录
     → launchctl bootout 旧服务
     → 原子替换 current.json
     → launchctl bootstrap 新服务
     → 检查 launchd 和 utun 地址
     → 删除旧配置目录
```

LaunchDaemon 设置 `RunAtLoad=true`、`KeepAlive=false`。禁用时由 `cleanup` 或 Apply 卸载服务并删除当前配置，这样停用状态的配置不会让 launchd 反复重启服务。

## 健康检查

健康检查只报告状态，失败或超时都不会停掉 sing-box。

- DNS 入口还没确认可用时，先不接管系统 DNS，每 5 秒重试一次。
- 已经接管后检查失败，sing-box 和 DNS 设置都保持不变，恢复正常后清除错误状态。
- 系统默认解析器里只能有 Steer 的 IPv4 或 IPv6 地址，不能混进别的 DNS。VPN 自己的作用域解析器不能拿来证明默认 DNS 已经接管。
- 主动停止或 sing-box 退出时，仍然会恢复 DNS。

## 不做的事

- 图形界面不处理流量，也不参与编译和 Apply；
- 不用 SmartDNS；
- 不照搬 Linux 处理转发 DNS 的 PREROUTING 规则；
- 图形界面不能绕过后台服务直接写配置目录或启动 sing-box。
