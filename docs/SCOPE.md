# 项目范围

本文描述 Steer 0.10、schema 9 的职责边界。共享核心定义用户配置语义，平台适配器负责操作系统资源和服务生命周期。

## 第一层：共享语义

共享核心提供：

- schema 9 Canonical Intent：主配置、Bootstrap、节点、订阅、逻辑路由、DNS Profile、本地代理和规则；
- 严格解码：UCI 适配器拒绝未知 section/option 和 scalar/list 形态错误；Canonical JSON codec 拒绝未知字段与尾随数据；
- 全局 ID、启用引用、协议参数、端口、URL、唯一 Direct、唯一 Default 等语义校验；
- first-match 规则，同字段 OR、不同字段 AND，Default 决定最终 DNS 和业务路由；
- 单节点路由前置链：目标必须存在、启用且同为 single，悬空、禁用、类型错误、自环和间接环全部拒绝；
- 确定性 sing-box 最终配置编译，包括 Route 私有出站、DNS 路径、Geo 引用和能力需求；
- 同步 Apply 生命周期 `Prepare → Activate → Healthy → Finalize` 与 `Disable`；
- HTTP(S) 订阅的抓取、解析、合并、稳定 ID、stale pin 和窄持久化接口；
- HTTP/TLS 测量、连接测试和完整下载报告格式。

共享核心不认识 UCI、procd、launchd、systemd、nftables、pf、路由表号或平台目录。`status` 返回当前运行配置身份、健康状态和最近 Apply 记录；配置合法性由独立 `validate` 返回。

## 第二层：平台实现

OpenWrt 适配器当前拥有：

- UCI schema 9 codec 和 UCI 订阅节点持久化；
- sing-box TUN `auto_route`/`auto_redirect` 入站；
- 传统 TCP/UDP 53 DNS 捕获和 sing-box 原生 `source_mac_address` 规则；
- nftables DNS shim、固定 TUN mark/table/priority/NFQUEUE 资源；
- procd 生命周期、开机私有 `_start` 钩子、本地健康检查；
- 包内完整 SRS seed、manifest 精确 selector 校验与 sing-box remote rule-set；
- `/run/steer` generation、`/var/lib/steer` 日志和订阅状态；
- LuCI、ucode RPC、ACL、OpenWrt 包和 cron 订阅调度。

这些是 OpenWrt 实现，不得反向污染共享 Intent。Linux 使用 JSON 配置、systemd 和 loopback Web；macOS 使用 JSON 配置、SwiftUI GUI、launchd 和 sing-box TUN。只要共享语义和 Apply 生命周期一致，平台目录、前端形式、网络接管方式、权限模型和服务管理器可以不同。

macOS 适配器当前拥有：

- Canonical JSON 配置、Darwin TUN `auto_route`、系统 DNS 接管与恢复，以及进入 TUN 的 TCP/UDP 目标端口 53 capture；
- root LaunchDaemon、generation、Geo seed、Apply/health/status/cleanup；
- SwiftUI GUI 配置与运维前端。GUI 与 LuCI、Linux Web 同级，只调用平台后端，不承载数据面。

## 交付与运行边界

OpenWrt 提供签名 APK 软件源，Linux 提供通用 tar.zst，macOS 提供 ad-hoc 签名 DMG 和首次安装系统组件的授权流程。具体平台与发布要求见 [打包与发布](PACKAGING.md)。

Apply 在切换前完成配置和环境检查；切换后的失败返回错误并保留现场，供诊断和重试。配置保存、运行态应用和订阅库存更新是独立操作，前端分别展示其结果。

新增共享字段需要同时更新 codec、校验、编译和三端表单。平台差异在适配器内实现；修改通过对应的行为测试和目标系统验证。
