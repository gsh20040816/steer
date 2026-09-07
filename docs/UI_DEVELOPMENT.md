# 原生前端与共享控制面开发约束

Steer 保留 OpenWrt LuCI、Linux Web 和 macOS SwiftUI 三种平台原生前端。三端可以采用不同的导航、布局、控件、鉴权与系统集成，但不得分别维护协议字段、配置约束、引用规则、操作结果或能力声明。前端差异只能来自平台呈现和明确的平台 capability，不能来自三份手工复制的产品规则。

## 分层

```text
Canonical Intent / Validate / Compiler
                  │
       shared UI specification
                  │ build-time generation
        ┌─────────┼──────────┐
        ↓         ↓          ↓
      LuCI     Linux Web   SwiftUI
        │         │          │
      ubus      HTTP      helper/socket
        └─────────┼──────────┘
                  ↓
          shared control contracts
                  ↓
       platform lifecycle adapters
```

职责必须保持如下边界：

- `intent` 和平台 `Validate` 是配置合法性的最终权威；UI 不复制第二套完整校验器。
- `internal/uispec` 是用户可编辑字段、控件类型、枚举、条件显示、敏感属性和平台 capability 的唯一 UI 规格源。
- 生成的 JavaScript/Swift 文件是构建产物，不得手工修改。修改必须从 `internal/uispec` 开始并重新生成。
- 平台 UI 只负责原生呈现、工作副本交互和调用平台 transport；不得决定 Apply、订阅合并、探测或 Geo 合法性。
- 平台 transport 可以不同，但同一操作必须使用相同的请求、结果和错误语义。

## 必须共享的功能规格

以下内容不得再分别硬编码：

1. 节点协议列表、显示名、字段、默认值、必填条件、枚举、TLS/transport 能力和切换协议时允许保留的字段；
2. Bootstrap、DNS Profile、本地代理和规则协议枚举；
3. 规则匹配字段、嗅探协议、Default 固定语义和 Geo 表达式前缀；
4. Direct/Reject/single Route 类型与 detour 约束；Reject 为兼容已有配置仍使用 `kind=block`；
5. collection 的稳定 ID 类型、只读来源、排序能力和删除引用策略；
6. capability 名称及不支持原因；
7. `saved`、`applied`、`revision`、`validation`、`status` 和结构化错误结果。

`id_policy` 规定常规对象由前端自动生成稳定 ID，`name` 只作为可选显示名；LuCI 仍使用 named UCI section，但 Add 不再要求用户发明 section ID。`creation_defaults` 与 `creation_required_fields` 规定新建对象必须真实写入 Draft/UCI 的字段，不能只靠控件 fallback 制造 phantom default。当前统一产品默认是 SOCKS/1080 节点、UDP/53 DNS Profile 和仅监听 loopback 的 Mixed/1090 本地入口；用户已有或显式修改的值不得被重新物化覆盖。同名引用只在发生歧义时追加 endpoint、来源和短 ID，普通名称保持简洁。

生成规格只能描述用户语义，不得包含 UCI section、systemd unit、launchd label、HTTP 路径、Unix socket 或 CSS/SwiftUI 布局。

## 原生呈现保留项

- LuCI 继续使用 `form.Map`、`GridSection`、UCI pending changes、LuCI session 与 ACL。
- Linux 继续使用 loopback Web、Bearer token、ETag 和浏览器可访问性控件。
- macOS 继续使用 `NavigationSplitView`、`Table`、`Form`、系统管理员授权和原生系统组件安装页。
- 三端自行决定字段如何分 tab/section、列表密度、弹窗、拖拽和平台帮助文本。
- CSS、SF Symbols、LuCI theme 和平台 shell 不做源码统一。

## 用户可见信息硬限制

普通前端是配置和运维界面，不是后端结构、诊断 DTO、日志归档或内部状态的转储器。一个字段已经脱敏、能够通过公开接口返回，只能说明它可以安全传输，不能说明它适合直接展示给用户。所有新增或修改的用户可见内容都必须先证明它能帮助用户完成当前页面的判断或操作；不能证明的内容不得进入普通界面。

以下内容默认禁止出现在总览、基础设置、对象列表、对象编辑器和普通状态栏：

1. 原始 JSON、数组、结构化 RPC/HTTP 响应、逐项诊断 DTO、完整错误链或未经归纳的日志行；
2. Canonical/UCI 内部 ID、digest、generation 路径、临时核心名称、outbound tag、进程参数、命令行、socket、unit/label 等实现标识；
3. RFC3339 纳秒时间、原始 URL path/query、空值占位字段、对用户没有行动意义的计数或后端阶段名；
4. 不设上限的历史记录、重复 Warning、每个实体一条的同质状态卡片，以及为了“信息完整”而默认展开的技术详情；
5. 用长篇帮助文字解释内部流程、数据结构或失败链路。界面文案只说明当前状态、影响、用户下一步以及必要的产品边界；
6. 凭据、私钥、Token、订阅正文和其他 sensitive 字段。脱敏占位符也不得被当作展示这些字段存在形式的理由。

用户可见事实必须转换为稳定的产品语义：优先显示用户命名、协议/endpoint 等可识别摘要、成功/失败、影响范围、发生时间和下一步操作。时间使用本地化或相对时间；不存在的字段直接省略；机器输入使用等宽字体，普通说明使用平台系统字体。自定义容器必须提供稳定的内部边距，文字不得贴住页面、卡片、状态栏或弹窗边缘。不得用 `visibility: hidden`、透明文字或离屏定位隐藏重复内容却继续保留其布局空间；应删除重复来源或让唯一来源承担真实布局与可访问名称。

最近 UI 开发形成以下强制收敛规则：

- Warning 必须先按候选配置或 Active 配置的实际可达运行图过滤，完全未被使用的节点、路由、DNS Profile 等实体不产生运行 Warning；普通界面按稳定类型聚合并显示数量，不逐条铺开同类问题。Error 仍保持严格校验，不得借此隐藏。
- 共享校验结果必须输出 `warning_groups`，其稳定分组键为 `code/object_type/option`，并携带受影响实体数、安全摘要和共享页面目标。三端 Overview 只能本地化这些分组和执行页面跳转，不得读取 `object_id`、raw `message` 或自行重建 Rule → DNS Profile/Route → Node/Detour 可达图；`main.enabled=false` 时实体运行 Warning 为空。
- probe 后端可以保留完成测试所需的结构化事实，但普通界面不展示连续历史报告。每个 overview/node/route 测试入口只持久显示对应 kind 的最近时间、结果和一个核心指标；配置身份变化后只标记“已过期”。
- 最近测试结果必须由共享 Go 层生成 `scope/object_id/kind/tested_at/ok/stale/summary/metric_value/error_summary` DTO，并由三端独立的批量读取 capability 提供。前端本地化时间、展示摘要，并用可选的 `metric_value` 排序：连接为毫秒，下载为 Mbps。未保存的 Draft 本身不改变测试所依据的 Saved identity，不能由前端擅自标记 stale。测试失败后仍必须立即安装动作返回的 DTO，或按键重新读取后端结果。
- 全局状态区域只显示运行状态、工作副本/Saved/Active 的必要差异和可执行操作，不显示 digest、generation、candidate 路径或内部 apply 阶段。状态必须与操作位于同一清晰容器，不能退化成贴边的小字遥测行。
- 相同事实只保留一个主呈现位置。总览给摘要和跳转，实体页承载实体结果，系统页承载安装与运行事实，高级页承载明确请求的高级数据；不得在多个页面复制完整明细。

有限例外必须按页面职责解释，不能扩张成通用兜底：

- 系统页可以展示排错、安装、Repair 或卸载所必需的版本、组件、配置目录、状态目录、socket 路径、服务标识和 Active generation 摘要。这些事实必须分组、命名、脱敏且可复制，不得直接倾倒后端对象或日志。
- 高级页可以在用户主动进入后展示 Canonical JSON 编辑/预览和必要的稳定内部标识；敏感字段仍默认隐藏，原始错误链、命令行和无限日志不因“高级”而自动允许。
- 日志只能作为明确的二级排错入口按需加载，必须脱敏、限量并保留来源；日志不得默认展开，也不得替代面向用户的错误摘要和恢复操作。

Code review 和契约测试必须拒绝以下实现：把后端对象直接传给通用 fact/card renderer、对 reports/warnings/logs 无界 `map` 到 DOM、在普通页面新增内部 ID/digest/path、用 CSS 截断掩盖冗长内容、或只删除文案但继续保留没有用户目标的空容器。至少应使用大订阅 Warning、重复 probe 报告、长错误链、空字段和含敏感值 fixture 验证普通 DOM 只包含有界安全摘要；系统页和高级页的例外单独测试，不得反向放宽普通页面。

## 统一信息架构

三端必须保持相同的页面职责和默认顺序。Linux 与 macOS 可以显示分组标题；LuCI 必须把分组层折叠为 Steer 下的一层页面菜单，避免主题导航、产品分组和表单 tab 叠成三层。用户不能因为换平台而重新学习功能位于哪里。

```text
状态
  └─ 总览
配置
  ├─ 基础设置
  ├─ 节点
  ├─ 路由
  ├─ DNS Profile
  ├─ 本地代理
  └─ 规则
服务
  ├─ 订阅
  ├─ 诊断
  └─ 系统
高级
  └─ 高级配置 / Canonical 预览
```

- 总览固定为五个语义区域：执行模型、配置生命周期、当前 Draft 的六类对象规模、当前 Draft 校验与聚合 Warning、最近 Apply 与快捷操作。六类规模固定为 Node、Route、DNS Profile、Local Proxy、Rule、Subscription；Saved/Active 与 pending Apply 必须来自平台后端真实状态，不能由 Draft 或最近 Apply 冒充。时间必须本地化，失败只显示安全可操作摘要；Save、Apply Saved、Save and Apply、Discard 可以由同页全局状态区承载，不得复制第二套开关或动作状态。
- 基础设置只编辑 Main、探测目标、DNS cache 和 Bootstrap。
- 节点与路由必须是独立页面；订阅状态和更新不能塞入节点页。
- 诊断统一容纳 overview 测试操作及最新安全摘要、Validate、Active port-53 配置检查、最近 Apply 和按需加载的受限日志，并提供 Refresh；不得展示 overview/node/route 连续历史报告。Node/Route 最近测速结果回到对应实体操作旁。
- 系统统一展示版本、schema、generation、last Apply、Geo seed、sing-box build tags、DNS capture boundary、平台路径和平台特有组件/安装能力。
- 高级页在 JSON 平台提供 Canonical 编辑，在 OpenWrt 提供 Canonical 只读预览；它不能成为结构化 UI 缺字段的常规兜底。
- 平台特有能力放在最接近的共享页面内，不得创建只在单一平台存在的顶层导航层级。

生成规格必须同时输出这份导航元数据；三端可以翻译标题或使用不同图标。Linux/macOS 的分组、三端页面 key 与顺序必须通过契约测试保持一致；LuCI 的菜单契约是按生成分组顺序展开后的单层叶子列表，不得再为分组创建可访问 URL。

## 功能基线与 capability

三端必须实现的共享基线：

- 状态、启用/禁用、Validate、Save、Apply 以及 Apply 部分成功的真实反馈；
- Main、Bootstrap、节点、路由、DNS Profile、本地代理、规则和订阅的结构化编辑；
- 所有当前 Canonical 节点协议的可完成原生表单；
- 节点导入、Geo catalog、订阅状态/更新/stale 清理；
- direct/proxy/download、单节点、批量节点和 Route chain 探测；
- generation、last Apply、Steer/sing-box/schema/Geo 运行事实；
- revision conflict 或与平台单写者模型等价的明确保护。

平台差异必须通过 capability 明确表达：

- macOS 不支持 `source_mac_address`，UI 必须说明原因，不能伪造支持；
- Canonical JSON 原文编辑只适用于 JSON 真相源；OpenWrt 提供 Canonical 只读预览，UCI 仍是唯一真相；
- macOS 系统组件安装/升级只属于 macOS；
- macOS 接管物理网络服务的系统 DNS；进入 TUN 的流量按“目标端口 53 劫持 → 私网 Direct → sniff → resolve → 用户规则”处理，网络服务变化由平台 DNS 看护处理；
- LuCI ACL、Linux Bearer token 和 macOS peer credential 是 transport 权限，不进入共享 Intent；
- 日志来源和订阅调度器属于平台实现，但用户操作和结果合同保持一致。

生成规格保存前端实际使用的字段、默认值、导航、引用关系和能力信息。页面行为由 Node 和 Swift 测试验证；生成文件和 LuCI 菜单通过一致性检查。

UI 不得显示后端未声明的操作。不可用能力应隐藏或禁用并给出稳定原因；不得用一段说明文字冒充已实现功能。

## 列表与操作一致性

- 节点必须按“手动节点 / 订阅”分组；订阅节点只读，不得显示可用但无效的编辑、删除或状态开关。
- 节点行和 Single Route 行必须同时提供连接测试与下载测速；批量测试只针对当前可见分组中已启用的节点。
- 单行测试、批量测试、订阅更新和 stale 清理不得禁用整张表或阻断滚动；只禁用当前正在执行的操作。disabled 对象不得提供后端必拒绝的动作；LuCI 存在 pending UCI 时不得用 committed Node/Route 执行测试。
- 三个概览测试读取 Saved URL，并直接使用设备当前网络环境；不得因 Steer 未启用或没有 Active generation 而禁用。Overview、Node 和 Route 的测试入口分别持久显示本入口最近一次测试的本地化时间、成功/失败和一个核心指标；配置身份变化后标为“已过期”。Diagnostics 不展示历史报告列表；临时核心名称、outbound ID、命令行、原始 URL、digest/generation 和完整后端错误链不得进入普通测试结果 UI。
- Direct 是系统必需且始终启用的固定路由；Reject 是固定类型但可启停。二者均不得显示删除、拖拽排序或类型转换操作，新建路由只能是 Single。Reject 只能编译为 sing-box route/DNS `reject` action，不得生成已废弃的 `type=block` outbound。
- 删除订阅时必须先检查其节点是否被 Route 引用；无引用时订阅与其生成节点必须一起从工作副本移除。
- 订阅 Update 的非阻断提示必须同时展示 added/current/stale/skipped，并明确“库存已更新、当前 Active 配置未改变、无引用的消失节点已自动删除、被 Route 引用的消失节点保留为 stale”；stale warning 提醒尽快解除 Route 引用，cleanup 只能移除已经解除引用的 stale 节点，不得级联改写 Route。
- 节点导入与导出统一使用共享后端解析器/序列化器：导入支持多行分享链接和 Base64 包装文档，导出使用当前工作副本并展示完整凭据警告；文案不得声称在前端本地解析或拼接链接。
- LuCI 批量节点导入在写入 pending UCI 前必须逐项展示名称、协议、endpoint、TLS 校验状态与真实的凭据存在性；凭据内容不得进入预览 DOM，Cancel 不得创建任何 section。JSON boolean 与 UCI `"1"` 必须使用同一 flag normalization。
- LuCI Named `GridSection` 必须保留原生 provisional section → editor modal → Save/Cancel 生命周期。ID 按共享策略自动生成，共享默认值在原生 `data.add()` 边界注入；Add 后立即编辑，Cancel 删除 provisional section，不能先 `map.save()` 留下空 pending row。
- `collection_references` 是三端删除保护的唯一关系表：Node ← Route.node、Route ← Rule.route/Route.detour、DNS Profile ← Rule.dns_profile、Local Proxy ← Rule.inbound。删除前必须列出引用并能跳转；订阅与 stale 节点复用 Node 引用保护，后端 Validate 仍是最终防线，不做级联删除。
- Rule 摘要必须覆盖全部 `rule_match_fields`。`rule_connection_only_fields` 明确 `ip_match/network/protocol/port` 不参与 DNS Profile 选择；只有这些条件时三端都显示“DNS 继续匹配后续规则”。macOS 必须显示共享 capability 中 Source MAC 不可用的稳定原因，不能静默隐藏。

## 敏感数据

- 分享 URL、节点密码、私钥、Web token 和订阅正文不得进入日志。
- 敏感正文不得通过进程参数传递；只允许请求正文、标准输入、权限受限临时文件或已鉴权本地 socket。
- 导入预览默认隐藏凭据；用户确认后才能进入工作副本。
- 生成规格只能标记字段为 sensitive，不能携带任何配置值。

## 操作结果

所有写操作必须区分持久化与运行态切换：

```json
{
  "saved": true,
  "applied": false,
  "revision": "sha256-…",
  "validation": { "ok": true, "errors": [], "warnings": [] },
  "apply_result": { "ok": false, "error": "…" }
}
```

如果配置已经保存而 Apply 失败，UI 必须明确报告部分成功，并把工作副本 revision 更新到磁盘事实。不得恢复成“未保存”状态，也不得暗示旧配置仍是持久化真相。

三端全局 Enable 只操作最新 Saved，并在与订阅更新共用的锁内执行读取、修改 enabled、保存和应用。Draft 格式错误不阻止启停。无修改的 Draft 在操作完成后同步返回的 Saved；有修改的 Draft 保留原始版本，后续显式保存仍检查冲突。

Linux Web 将 Draft、Saved 与 Active 分开显示：`dirty` 只表示浏览器工作副本，revision 属于已保存配置，Active generation/digest 只来自 `/run/steer/current`。Save 后即使 Draft 已 clean，只要已保存配置的编译运行投影与 Active 不同，或最近一次同投影 Apply 失败，全局 `Apply 已保存配置` 仍保持可用。订阅刷新产生但未被 Route 引用的节点库存不进入该运行投影，只显示库存 warning，不制造 pending Apply。

Linux Advanced JSON 与结构化页面必须共享同一个 Draft。textarea 输入立即进入 store 并触发 dirty；语法无效时保留原文、阻止 Save 和结构化导航，不能退回一份旧的解析对象冒充当前表单。顶部与 Advanced 页内的 Save / Save and Apply 调用同一动作。dirty 时全局必须提供带确认的“放弃修改”，确认后通过唯一 reload 路径同步 Intent、JSON 文本、revision、overview 与当前页面；取消不得改变 Draft。

Linux store 为每次 Draft mutation 分配递增 epoch。Save 必须发送不可变快照；响应只允许清理与请求 epoch 相同的 Draft，期间新增修改继续保持 dirty。Save、Apply Saved 与 reload 互斥，重复点击或乱序响应不得覆盖较新的 Intent/revision。订阅 update/clean 同样记录开始 epoch；若请求期间 Draft 变化，只刷新 inventory 提示，不自动 reload，也不得让离页后的旧 render 覆盖当前路由。

Save/Apply 写结果必须携带完整 `validation.errors/warnings`（包括 `code/object_type/object_id/option/message`），但传输合同不等于展示合同。三端结果面板绑定当前 Draft epoch；Draft 改变立即丢弃旧“通过/失败”。Error 动作必须打开对应对象并定位字段；Warning 先按实际可达运行图过滤，再按稳定类型聚合为用户摘要。普通 DOM 不得渲染内部 object ID、原始 message 数组或凭据值。

Linux 页面可见时低频刷新服务器 Saved revision 与 Active status。外部 revision 与 Draft 基线不同时只能设置冲突事实：dirty Draft 不得被替换，clean Draft 由用户显式一键 reload。显式 Refresh、周期刷新和 Save 后 overview 刷新必须走同一比较语义。

最近 Apply 是独立的操作记录。后端保留 candidate 身份、时间、成功/失败和错误摘要；普通 UI 只持久显示本地化时间、结果和可操作的安全摘要，candidate 路径或内部 generation 只允许按系统页职责展示。candidate 不得作为 Active generation 的兜底来源。

macOS Load 必须同时返回 Saved revision，Save/Apply 必须携带 `expected_revision`。revision conflict 不得修改 Saved、Active 或本地 Draft；UI 必须提供 Reload Saved、保留本地 Draft 和显式覆盖。订阅手动更新完成时只能在 Draft 未发生变化的情况下自动 reload，否则保留 Draft 并进入同一冲突选择；订阅库存变更始终不自动 Apply。

macOS 每个 App 生命周期只初始化一次 Draft。所有页面与菜单栏共用 Save、Apply Saved、Save and Apply；Apply Saved 只部署磁盘 Saved，不得夹带 dirty Draft。窗口 toolbar 在所有页面提供全局 Enable；切换时通过独立 control 命令，只修改最新 Saved 的 enabled 并应用，未保存的 Draft 保留原文和原始 revision。状态查询同样经 control 服务读取受保护的 Active 文件，读取失败不能当作服务停止。Reload、安装/Repair 和退出等会替换或结束 Draft 的动作必须走同一个 Save / Discard / Cancel guard。安装完成默认保留编辑中的 Draft，Apply 失败时必须分别显示 Saved 开关与后端实际 Active 状态。

LuCI 必须从当前 rpcd session 的 candidate、committed UCI 与 `/run/steer/current` 分别构造 Pending desired、Saved 与 Active。每个 Steer 页面共用包含全局 Enable 的状态区域；切换时通过独立 RPC 只修改 committed UCI 的 enabled，并直接 Apply；不保存页面表单或 session pending UCI，基础设置页不再维护重复的服务总开关。pending disable 不能隐藏仍运行的 Active generation；失败 Apply 在无 pending UCI 时必须提供 Apply Saved 重试。`pending_apply` 比较编译运行投影，不能由全文 Intent digest 推导，因此未引用的订阅节点库存变化不制造 pending Apply，也不产生运行 Warning。

## 生成与测试门

`go generate` 生成 LuCI、Linux Web 和 SwiftUI 使用的只读规格。提交必须包含已更新的生成文件。CI 必须检查重新生成后工作树无差异，并验证：

1. 每个可选择节点协议都能由规格构造通过共享校验的代表配置；
2. Canonical 的每个用户字段都被规格覆盖或标为内部/平台不支持；
3. 三端生成文件拥有相同的规格 schema 与 digest；
4. capability 为 true 的操作存在平台后端契约测试；
5. 可见操作不得缺少后端实现；
6. OpenWrt、Linux 和 macOS 的原生 UI 回归测试继续通过。

新增或修改字段时，正确顺序是：Canonical model/Validate → `internal/uispec` → 重新生成 → 三端原生布局接入 → 契约测试。不得先在某一个前端添加字段再补其他平台。
