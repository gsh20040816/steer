# ADR 0003：订阅节点独立存储，运行配置不变时不重启

## 状态

已采纳，从 0.12.0（schema 10）开始生效。

## 背景

- 每次应用都会重启 sing-box。`apply.Run` 在准备好候选配置后，总是调用平台的 `Activate`，而三个平台的 `Activate` 都是先停服务、再发布、再启动（OpenWrt 用 init 脚本，Linux 用 systemctl，macOS 用 launchd）。编译器已经算出了只包含实际生效内容的 `RuntimeDigest`，但应用流程没有用它判断要不要重启。
- 订阅节点写在 Saved 配置里（OpenWrt 的 UCI、Linux 和 macOS 的 config.json）。每次订阅刷新都会改动用户配置：UCI 文件里大部分是订阅节点，OpenWrt 上配置一变就可能触发 procd 的重载，进而触发一次应用和重启。Linux 和 macOS 上，后台刷新订阅还会和界面编辑抢配置的 revision。
- 订阅合并本身已经保证被路由引用的节点内容不变：内容变了会生成新 ID 的节点，被引用、但上游已经删除的旧节点原样保留并标记 `pinned_stale`。所以订阅刷新从不改变实际运行的配置。

## 决定

### 一、运行配置不变时不重启

`apply.Backend.Activate` 返回是否重启了服务。平台在启用前比较候选配置和当前运行配置：

- 当前运行配置存在且服务健康（平台对当前运行配置做一次现有的单次健康检查：OpenWrt 的 procd sing-box 实例在运行、TUN、nftables 和 DNS 监听就绪；Linux 的 systemd 服务处于 active、TUN、nftables 和监听就绪；macOS 的 LaunchDaemon 在运行、`current.json` 和元数据有效、utun 和 DNS 接管就绪），并且候选配置和当前配置里会影响运行的文件逐字节相同（`sing-box.json`，以及平台自己的运行文件：OpenWrt/Linux 的 `platform.json`、`firewall.nft`，macOS 的 `platform.json` 和 `generation.json` 里除生成 ID 和配置摘要以外的部分），这时只把 `current` 原子地切换到候选配置，不停止、不启动服务，也不重新加载 nftables。
- 其他情况照旧：停服务、设置平台资源、切换 `current`、启动。

切换后照常执行健康检查和收尾。应用结果新增 `restarted` 字段（布尔值，总是输出），说明这次应用是否重启了服务；macOS control 的 `apply`/`set-enabled` 响应也带上完整的 `apply_result`。macOS 的看护进程原来用运行配置的路径标记 DNS 接管（`dns-ready.json`）和 WireGuard 状态，改为记录 `sing-box.json` 的内容摘要，这样切换到等价的运行配置、旧目录被清理后，这些标记仍然对应正在运行的 sing-box。`intent.json` 不参与比较，所以只改了配置里不影响运行的部分（例如节点名、未被引用的节点、订阅元数据）时，应用会更新当前配置的记录，但不重启。

这条规则对所有触发应用的入口都生效，包括 LuCI 保存并应用、procd 重载、Linux 网页、macOS 界面和命令行。

### 二、订阅节点不再写入配置

订阅节点只保存在平台的订阅节点存储里，Saved 配置只保存手动节点、订阅定义、路由和其他用户设置。

存储位置（必须是持久存储；OpenWrt 的 `/var` 在内存里，不能用）：

```text
OpenWrt  /etc/steer/subscription-nodes/<订阅 ID>.json
Linux    /var/lib/steer/subscription-nodes/<订阅 ID>.json
macOS    <平台状态目录>/subscription-nodes/<订阅 ID>.json
```

文件格式：

```json
{
  "schema_version": 1,
  "subscription_id": "jdub",
  "nodes": [ { "...": "完整的节点字段，含 source_subscription、source_fingerprint、pinned_stale、enabled" } ]
}
```

- 文件权限 0600，原子写入（临时文件、fsync、rename）。内容没有变化时不写，避免无谓的闪存写入。macOS 例外：和 `config.json` 一样交给 `root:admin 0640`，图形界面不用提权就能运行 `subscription status`。
- 每个节点的 `source_subscription` 必须等于文件的订阅 ID。
- 订阅状态快照（`/var/lib/steer/subscriptions/<id>.json` 等现有位置）只保存获取状态、跳过原因、新增数量和最近失败，不再保存节点，避免把凭据复制到第二个地方。

共享代码 `internal/subscription` 提供存储的读写实现和一个加载函数（`subscription.Attach`）：平台读取 Saved 配置后，先检查配置里没有订阅节点（`SUBSCRIPTION_NODE_IN_CONFIG`），再把所有已配置订阅（无论是否启用）的存储节点附加到配置里，得到完整的 Saved Intent。校验、编译、测速、状态和 `pending_apply` 都基于这个完整的 Intent。运行配置里的 `intent.json` 保存的也是完整 Intent。存储里有、但 Saved 配置里已经没有对应订阅的文件，不参与加载，在下一次成功的保存（Linux 网页、macOS control）、应用或订阅更新后删除；三端规则相同，OpenWrt 上 LuCI 直接写 UCI，所以靠应用和订阅更新回收。

### 三、订阅节点的操作

订阅节点由订阅管理，界面上只读，只允许：

1. **启用/停用**单个订阅节点：写存储，不碰配置。写入前用改动后的完整 Intent 校验，停用正被启用的路由引用的节点会被拒绝（和现在的校验规则一致，错误码 `NODE_STILL_REFERENCED`）。
2. **清理过期节点**：删除 `pinned_stale` 且没有被引用的节点（现有 `subscription clean` 的语义），写存储。
3. **复制到我的节点**：三端界面都必须提供。用户把一个订阅节点复制成手动节点，在自己的节点列表里自由编辑。复制结果是配置里的一个新节点：新 ID，名称默认沿用原名，保留全部协议字段和凭据，去掉 `source_subscription`、`source_fingerprint`、`pinned_stale`，启用状态为启用。复制规则由共享代码定义（`subscription.CopyAsManual`），界面只负责触发和打开编辑。复制不改变原订阅节点，也不会自动把路由改指向新节点。

订阅刷新：下载、解析，和存储里的现有节点按现有 `Merge` 规则合并（路由引用来自 Saved 配置），校验完整 Intent，然后只写存储和状态快照。不写配置，不自动应用。

删除订阅：界面只删除配置里的订阅；保存成功后，后台删除这个订阅的存储文件。被删除订阅的节点如果还被路由引用，保存前的校验会拒绝（和现在删除订阅时同时删除节点的行为一致）。

### 统一原则

三个平台的行为必须一致，能共享的实现只写一份：

- 判断逻辑都在共享 Go 包里：运行配置是否相同（`generation` 包提供比较函数，平台只告诉它哪些文件属于运行配置、服务是否健康）、订阅节点存储的读写和加载、启用/停用前的校验、清理规则、复制为手动节点、schema 9 到 10 的节点迁移。平台代码只负责路径、加锁、服务状态和把配置写回 UCI 或 JSON。
- 三端接口的请求和返回格式相同，字段名相同，错误码相同。
- 三端界面的表现相同：订阅节点按订阅分组、只读显示；每个订阅节点都有「启用/停用」和「复制到我的节点」；过期节点有「清理」；应用结果在没有重启时显示同样的提示「运行配置没有变化，未重启」。文案三端一致，LuCI 的英文原文和中文翻译、Linux 网页、macOS 界面使用同一套措辞。

### 四、各平台的接口

所有写接口都在现有的操作锁里执行（OpenWrt 的订阅更新、清理和启用/停用也改为拿 `/run/steer/apply.lock`）；读取接口不拿锁。三个平台的命令行也都有 `subscription nodes [--id]` 和 `subscription node --id --node --enabled 0|1`，输出和下面的接口相同。

| 操作 | OpenWrt（ucode RPC，经命令行实现） | Linux 网页 | macOS control |
|---|---|---|---|
| 读取订阅节点 | `subscription_nodes`，调用 `steer subscription nodes` | `GET /api/v1/subscription-nodes` | `subscription-nodes` |
| 启用/停用 | `subscription_node_set {id, node, enabled}`，调用 `steer subscription node --id ID --node NODE --enabled 0\|1` | `PUT /api/v1/subscriptions/<id>/nodes/<node>`，请求体 `{"enabled": true}` | `subscription-node-set`（ID、NodeID、Enabled） |
| 清理过期节点 | 现有 `subscription_clean` | 现有接口 | 现有 `subscription-clean` |
| 复制到我的节点 | 界面把共享规则生成的节点加入 UCI 草稿 | 界面把节点加入配置草稿 | 界面把节点加入配置草稿 |

复制到我的节点只是一次普通的配置编辑，不需要单独的后台接口。为了保证三端结果一致，读取接口返回的每个订阅节点额外带一个 `manual_copy` 字段，内容就是共享代码生成的手动节点（不含新 ID，由界面按各自的 ID 规则分配）。

读取接口返回 `{"subscriptions": [{"id": "...", "nodes": [节点...]}]}`，节点字段和配置里的节点字段一致（含凭据，界面需要它们来显示、导出和复制），额外带 `source_subscription`、`source_fingerprint`、`pinned_stale` 和 `manual_copy`（没有 `id` 键）。只返回 Saved 配置里存在的订阅，可以用订阅 ID 过滤（OpenWrt `--id`、Linux `?id=`、macOS 请求里的 `id`），不存在时报 `SUBSCRIPTION_NOT_FOUND`。启用/停用成功时返回同样格式的完整列表。

失败时三端都返回 `{"ok": false, "error_code": "…", "error": "…", "validation": {…}}`（macOS 放在 control 响应的同名字段里，Linux 用 404/422/500 状态码）。错误码：`SUBSCRIPTION_NOT_FOUND`、`SUBSCRIPTION_NODE_NOT_FOUND`、`NODE_STILL_REFERENCED`、`SUBSCRIPTION_NODE_CURRENT`（清理当前节点）、`VALIDATION_FAILED`（含配置里有订阅节点的情况）、`SUBSCRIPTION_NODE_STORE_FAILED`。

配置读取接口（UCI、`GET /api/v1/config`、macOS 的 `state`）返回的配置不含订阅节点。校验和预览接口在内部附加存储节点后再校验，所以界面提交的草稿里不需要、也不允许包含订阅节点。

### 五、schema 10 和迁移

- `intent.SchemaVersion` 改为 10。配置里的节点不允许带 `source_subscription`、`source_fingerprint`、`pinned_stale`（校验错误 `SUBSCRIPTION_NODE_IN_CONFIG`）。
- 迁移把 schema 9 配置里所有带 `source_subscription` 的节点按订阅分组写入存储（保留 ID、启用状态、指纹、过期标记和顺序），从配置里删除这些节点，再把 schema 改为 10。迁移是幂等的：已经是 schema 10 的配置不做任何修改，其他 schema 也不动，交给校验报错。先写存储再写配置，中途中断时下次重新迁移。
- schema 9 不检查 `source_subscription` 是否指向已配置的订阅。来源订阅已经不在配置里的节点如果写进存储，就不会再被加载，引用它们的路由会失效，所以这类节点留在配置里变成手动节点（保留 ID 和启用状态，去掉三个来源字段）；没有 `source_subscription` 却带着另外两个字段的节点同样去掉这两个字段。
- 迁移同时去掉 schema 9 状态快照里复制的节点列表。
- 迁移由内部命令 `steer _migrate` 执行，不是公开命令：
  - OpenWrt：安装或升级软件包时由 uci-defaults 脚本（`/etc/uci-defaults/90-steer-schema-10`）调用；安装后脚本在 `steer apply` 之前也调用一次，不依赖两个钩子的先后顺序；`_start` 发现 schema 9 时也先迁移再启动，避免升级后第一次开机失败。`/etc/steer/subscription-nodes/` 加进 `/lib/upgrade/keep.d/steer`，刷固件时保留。
  - Linux：`_run`（服务启动）和 `web` 启动时发现 schema 9 先迁移；Arch 包的升级钩子也调用一次。
  - macOS：LaunchDaemon 的 `_run` 和 control 服务启动时发现 schema 9 先迁移。
  - 启动入口只在发现 schema 9 时才拿操作锁：应用会在持锁期间启动服务，`_run` 不能每次都等锁。`_run` 迁移失败时退出；Linux 网页和 macOS control 服务只记录错误并继续启动，界面照常显示校验错误。
- 迁移前后，同一份配置编译出的 `sing-box.json` 必须完全相同。

## 后果

- 订阅刷新不再改动用户配置，OpenWrt 上也就不会因此触发 procd 重载；即使触发了应用，只要运行配置没变也不会重启 sing-box。
- 所有平台上，保存不影响运行的修改（节点改名、增删未被引用的节点、订阅设置等）都不会再中断代理。
- 订阅节点不能再在界面里直接编辑字段。需要改动时，复制为手动节点再改。
- 0.12.0 不能直接回退到 0.11.x：schema 10 的配置在旧版本上会被拒绝。需要回退时，先用 0.11.x 恢复 schema 9 的配置备份。

## 实现说明

- 「服务健康」用的是平台现有的单次健康检查，比上面列举的条件更完整；当前运行配置不完全健康时走原来的重启路径。
- 比较函数是 `generation.Unchanged`，`sing-box.json` 由共享代码固定比较，平台只传自己的运行文件（可附带去掉生成 ID 的规范化函数）和健康检查；切换 `current` 链接的 `generation.PublishLink` 也由 OpenWrt 和 Linux 共用。
- 配置读取以外的入口全部用完整 Saved Intent：OpenWrt 的 `validate`、`apply`、`_start`、`_state`、`_export-intent`、`_export-node`、`probe`、`_diagnostics`、`_probe-results` 新增 `--node-dir`（默认 `/etc/steer/subscription-nodes`），LuCI 的候选配置校验也因此会拼上存储节点；Linux 和 macOS 从 `--state-dir` 推出存储目录。
- 共享代码里的订阅更新、清理、启用/停用、读取列表、复制规则和迁移计划分别是 `subscription.Update`、`CleanStale`、`SetNodeEnabled`、`ListNodes`、`CopyAsManual`、`PlanMigration`/`WriteMigratedNodes`；错误载荷由 `subscription.ErrorPayloadOf` 统一生成。
- 界面共用的文案和字段放在界面描述的 `subscription_nodes`（只读、`manual_copy`、四个操作、每个错误码的提示、`SUBSCRIPTION_NODE_IN_CONFIG`、只读说明、读取失败、空分组、过期标记、清理结果和路由名称的分隔符）和 `apply_result`（`restarted` 字段、显示提示的条件、提示文案）里。每条文案都是 LuCI 的英文 msgid 加 `zh_cn`，LuCI 的中文翻译必须和 `zh_cn` 完全相同；`NODE_STILL_REFERENCED` 的提示用 `{routes}` 占位，界面换成仍在使用这个节点的路由名称。没有列出的错误码显示后端的 `error`。
- 因为订阅节点的操作不改配置，三端界面的放行规则相同：启用/停用和更新不看草稿或 UCI 里有没有未保存、未提交的修改（rpcd 的 `subscription_update`、`subscription_clean`、`subscription_node_set` 也不再要求会话里没有未提交的修改）；完成后只重新读取订阅节点列表和订阅状态，不重新载入或覆盖草稿，也不弹出版本冲突；清理只在当前草稿或已保存配置里有路由引用这个节点时阻止，并列出这些路由。后端照旧只按 Saved 配置和节点存储校验。具体约定见 `docs/UI_DEVELOPMENT.md` 的「订阅节点（三端一样）」。
- 配置里的节点都是手动节点，界面描述的节点排序策略不再按 `source_subscription` 分组。共享样例 `ui/*-fixtures.json` 的配置对象只放手动节点，订阅节点放在单独的 `subscription_nodes` 里，格式和读取接口的返回完全一样。
