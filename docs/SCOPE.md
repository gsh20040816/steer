# 项目范围

这篇说明 Steer（0.11，schema 9）里哪些事归共享核心管，哪些归各平台自己管。简单说：配置是什么意思由共享核心决定，怎么落到操作系统上由平台适配器决定。

## 共享核心

共享核心负责：

- schema 9 的配置格式（Canonical Intent）：全局设置、引导 DNS、节点、订阅、路由、DNS 配置、本地代理和规则；
- 严格解码：UCI 里出现未知的 section/option 或者单值/列表写错，直接报错；Canonical JSON 出现未知字段或多余内容，也直接报错；
- 语义校验：ID 是否唯一、引用的对象是否存在且启用、协议参数、端口、URL，以及「恰好一条直连路由、恰好一条默认规则」；
- 规则匹配：按顺序命中第一条；同一个字段的多个值是「或」，不同字段之间是「且」；默认规则决定最终的 DNS 和路由；
- 前置代理链：前置路由必须存在、启用、类型是 single，不能悬空、不能指向自己、不能绕成环；
- 把配置确定性地编译成 sing-box 配置，包括每条路由的出口、DNS 路径、Geo 引用和所需的 sing-box 能力；
- Apply 流程 `Prepare → Activate → Healthy → Finalize`，以及 `Disable`；
- HTTP(S) 订阅：下载、解析、合并、保持节点 ID 稳定、保留被引用的过期节点；
- 网络测试和测速的测量方式与报告格式。

共享核心不知道 UCI、procd、launchd、systemd、nftables、pf、路由表号或任何平台目录。`status` 只返回当前运行的配置、健康状态和最近一次 Apply；配置是否合法由单独的 `validate` 回答。

## 平台适配器

**OpenWrt**

- UCI 格式的读写，订阅节点也存进 UCI；
- sing-box TUN 入站（`auto_route`/`auto_redirect`）；
- 53 端口 DNS 接管，来源 MAC 规则用 sing-box 原生的 `source_mac_address`；
- 一小段 nftables DNS 规则，以及固定的 TUN mark、路由表、优先级和 NFQUEUE；
- procd 服务、开机钩子、本地健康检查；
- 包内的 SRS 数据和 manifest、sing-box 远端规则集更新；
- `/run/steer` 下的运行配置，`/var/lib/steer` 下的日志和订阅状态；
- LuCI 界面、ucode RPC、ACL、软件包和 cron 订阅调度。

**Linux**：JSON 配置、systemd、只监听本机的网页界面。详见 [Linux](LINUX.md)。

**macOS**：JSON 配置、Darwin TUN、系统 DNS 的接管与恢复、root LaunchDaemon，以及 SwiftUI 图形界面。图形界面和 LuCI、Linux 网页界面是同一层的东西，只调用后端，不处理流量。详见 [macOS](MACOS.md)。

平台相关的东西不进共享配置格式。只要配置含义和 Apply 流程一致，各平台的目录、界面、接管网络的方式、权限和服务管理都可以不同。

## 交付和运行

OpenWrt 提供签名的 APK 软件源，Linux 提供通用 tar.zst，macOS 提供 ad-hoc 签名的 DMG，第一次安装系统组件时要管理员授权。细节见[打包与发布](PACKAGING.md)。

Apply 在切换之前做完配置和环境检查；切换之后才出错的，返回错误并保持现状，方便排查和重试。保存配置、应用配置、更新订阅是三件独立的事，界面分别显示结果。

新增一个共享字段时，要一起改解码、校验、编译和三端界面。平台差异放在适配器里，并用对应平台的测试验证。
