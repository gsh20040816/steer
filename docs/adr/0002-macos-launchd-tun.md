# ADR 0002：macOS 使用 LaunchDaemon + sing-box TUN

## 状态

Accepted；这是 macOS 唯一受支持的数据面架构。

## 背景

项目不购买 Apple Developer Program。sing-box 已经提供 macOS TUN 和
`auto_route`，Steer 不需要重新实现数据面。SwiftUI GUI 只作为配置与运维
前端，数据面继续由 root LaunchDaemon 管理。

## 决策

macOS 使用普通 sing-box 二进制，由 root LaunchDaemon 启动：

```text
steer-macos apply
        ↓
launchctl bootstrap
        ↓
steer-macos _run
        ↓
supervise sing-box + system DNS lease
        ↓
Darwin utun + auto_route
```

平台 plan：

- TUN inbound 不设置 `interface_name`，让 Darwin/utun 自动选择设备；
- 设置 `auto_route`、`stack=system`、MTU 和地址；
- 不再追加 IPv4 RFC1918、CGNAT 与 IPv6 ULA 大网段路由；
- `route_exclude_address` 继续保留回环、链路本地、组播和文档/保留地址，但不再排除上述私网；
- 不设置 Linux-only `auto_redirect`、iproute2 mark、nftables 或 pf。

DNS 由同一份 sing-box DNS Router 负责。TUN 使用 `dns_mode=hijack`，不显式填写 `dns_address`，让核心派生 `198.18.0.2` 与 `fdfe:dcba:9876::2` 并自动处理发往这些地址的 DNS。macOS CLI 缺少原生系统 DNS 设置，所以 `_run` 在 DNS 健康后按物理网络服务 UUID 保存并接管系统 DNS，退出时恢复；独立 control daemon 回收崩溃遗留 journal。恢复仅处理当前仍等于 Steer 写入值的服务，不覆盖用户后续修改；VPN 专用服务和搜索域保持不变。

route 第一条仍明确匹配 `inbound=steer-tun + network=[tcp,udp] + port=[53]` 执行 `hijack-dns`，第二条为私网 Direct。此规则只覆盖已经进入 TUN 的请求，不能修复直连/作用域路由绕过；系统 DNS 接管承担默认解析路径的修复。网络轮询只处理 OS DNS，不重新生成配置或隐式 Apply 草稿。

## 生命周期

`steer-macos apply` 通过同步 backend 完成：

```text
Validate → Compile → sing-box check → generation prepare
       → launchctl bootout old
       → atomic publish current.json
       → launchctl bootstrap new
       → check launchd + utun addresses
       → prune old generations
```

LaunchDaemon 使用 `RunAtLoad=true`、`KeepAlive=false`。禁用时由 `cleanup`/Apply unload 服务并删除 current generation，避免 disabled 配置造成 launchd 重启循环。

## 不做的事

- 不把 Swift GUI 放入数据面或 Apply 编译路径；
- 不引入 SmartDNS；
- 不让 macOS 复用 Linux 的 forwarded DNS PREROUTING 逻辑；
- 不让 GUI 绕过 helper 直接写 generation 或启动 sing-box。
