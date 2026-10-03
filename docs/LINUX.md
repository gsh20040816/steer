# Linux

Linux 版面向使用 systemd 的发行版。它接管本机流量，也接管经本机转发出去的虚拟机和 Docker 流量。本仓库只发布通用的 x86_64/aarch64 tar.zst 包，另外维护一份 Arch AUR 的源码配方；CI 不打 deb、rpm 或 `.pkg.tar.zst`。

## 工作方式

- **配置**：只有一份，`/etc/steer/config.json`，格式是 schema 9 的 Canonical JSON。没有额外的「平台设置」文件。
- **流量**：sing-box TUN 开启 `auto_route`、`strict_route` 和 `auto_redirect`。sing-box 不锁版本，Apply 时用它自带的配置检查判断当前版本支不支持用到的字段。来源 MAC 规则用 sing-box 原生的 `source_mac_address`。
- **DNS**：TUN 开启 `dns_mode: hijack`，普通的 TCP/UDP 53 查询和 systemd-resolved 的网卡 DNS 都由 sing-box 自己接管。Steer 只额外处理「发给本机地址的 DNS 查询」：
  - `PREROUTING` 处理局域网、虚拟机和 Docker 发给本机的查询；
  - `OUTPUT` 处理本机发给回环地址或自己网卡地址的查询（DNAT，必要时 SNAT）。
  - 两条链都以 `fib daddr type != local return` 开头，目标不是本机的查询直接交给 sing-box。
  - 转发目标是 IPv4 的 1053 端口和 IPv6 的 1054 端口，`input` 链只放行经过 DNAT 的连接。
  - 应用自己发的加密 DNS（DoH/DoT/DoQ）不在接管范围内。
- **ping**：走共享的三层策略，命中代理路由的 ping 改走直连。来源地址可路由的交给内核转发；来源是本机 TUN 地址的由 sing-box 直连出口重新选源地址。
- **IPv6 内核直连**：支持可选的 `direct_bypass`，见[配置说明](CONFIGURATION.md#ipv6-内核直连0110)。
- **服务**：systemd 的 `steer.service`。`steer _run` 做完准备后直接 `exec` 成 sing-box，`ExecStopPost` 调用 `steer cleanup` 清理。`steer.service` 声明了 `PartOf=nftables.service`，重启 nftables 时 Steer 会跟着重启并重建规则。
- **管理**：命令行 `steer`，以及只监听本机的网页界面 `steer web`。
- **订阅**：systemd timer 定时更新配置文件里的节点，不会自动 Apply。更新失败，或者返回 200 但没有一个有效节点时，保留原来的节点。
- **Geo 数据**：安装包里带了完整的 SRS 数据和 manifest。Steer 按 manifest 精确校验规则里引用的分类，sing-box 之后在后台从远端更新。

不支持的：非 systemd 系统、通用的局域网网关配置向导、多用户权限、远程访问网页界面、NetworkManager/systemd-resolved 深度集成、Clash API、实时连接图。TUN 不按网卡做白名单，所以转发的虚拟机/Docker 流量和本机流量走同一套规则；私网和链路本地目标照常排除在外。

## 安装

GitHub Release 提供：

```text
steer-linux-x86_64.tar.zst
steer-linux-aarch64.tar.zst
```

包里有 `steer` 可执行文件、systemd 单元、两个示例配置、许可证和 `geodata-seed/`。不包含 sing-box、geoview、DAT 数据库，也没有安装脚本。以 x86_64 为例：

```sh
tar --zstd -xf steer-linux-x86_64.tar.zst
cd steer-linux-x86_64
sudo install -m 755 steer /usr/bin/steer
sudo install -d -m 700 /etc/steer /var/lib/steer
sudo install -m 600 config.example.json /etc/steer/config.json
sudo install -m 600 web.example.json /etc/steer/web.json
sudo install -d -m 755 /usr/share/steer/geodata-seed
sudo cp -a geodata-seed/. /usr/share/steer/geodata-seed/
sudo install -m 644 systemd/*.service systemd/*.timer /etc/systemd/system/
sudoedit /etc/steer/web.json
sudo steer web-token  # 打印配置里的 token，粘贴到网页登录页
systemctl daemon-reload
systemctl enable --now steer.service steer-web.service steer-subscription.timer
```

也可以从源码 tag 构建：在 `go/` 目录运行 `CGO_ENABLED=0 go build -trimpath -o ../steer ./cmd/steer-linux`。发行版打包时请装到 `/usr/bin/steer`。

启用前，用系统包管理器装好合适版本的 sing-box、nftables、iproute2 和 ca-certificates。然后：

```sh
steer validate
steer apply
steer health
```

## 网页界面

网页界面默认只监听 `127.0.0.1:9080`，要远程用就走 SSH 端口转发，不支持绑定公网地址。

登录 token 只来自 `/etc/steer/web.json`（schema 1）里的 `token` 字段，要求 32–256 个可见 ASCII 字符、不含空格。`steer web-token` 只是把它打印出来，不会生成或迁移 token。

**启停和保存**

- 顶部状态栏有启用开关。切换后立即保存配置并 Apply；Apply 失败会提示，已保存的配置保留。禁用会停止服务、清理 nftables 规则并删除运行中的配置。
- 所有配置页共用「保存」「保存并应用」「应用已保存配置」三个按钮。「应用已保存配置」不看浏览器里有没有未保存修改，只要已保存的配置和正在运行的不一样，它就可以点。
- 有未保存修改时，顶部有「放弃修改」，确认后重新载入已保存配置。切换页面不会丢修改，刷新或关闭浏览器会提示未保存。

**编辑**

- 「高级」页的 JSON 文本框和结构化页面编辑的是同一份草稿，两边实时同步。JSON 写错时原文保留，但不能保存，也不能切到结构化页面。
- 保存时发送的是点击那一刻的草稿快照。保存期间继续编辑，新的修改不会被旧响应清掉。保存、应用、重新载入同一时间只能进行一个。
- 订阅更新或清理期间如果草稿变了，界面保留草稿并提示节点列表已变化，不会自动重新载入。

**状态和刷新**

- 页面可见时每 30 秒刷新一次已保存配置的版本和运行状态，顶部也有手动刷新按钮。
- 如果命令行、timer 或别的页面改了配置：有未保存修改时保留草稿并提示冲突；没有修改时提供一键重新载入。刷新运行状态不会自动替换草稿。
- 「当前运行」的配置只读 `/run/steer/current`。最近一次 Apply 单独显示时间、结果和错误摘要，失败的配置不会被当成正在运行的。
- 订阅列表区分「从未更新」「最近成功」「最近失败」，并显示跳过和过期的节点数。停用的订阅不能点更新。订阅更新只改了节点列表时，不会提示需要 Apply；消失且没被路由引用的节点直接删除，被引用的保留为过期并显示提醒。

**测试和诊断**

- 三个网络测试读取已保存配置里的网址，直接用本机当前网络访问，Steer 没启用也能测。
- 诊断页显示测试结果摘要、当前运行配置里的 53 端口接管检查，以及 `steer`、`steer-web`、`steer-subscription` 三个 systemd 单元的日志。53 端口检查只核对配置，不是抓包，不能证明加密 DNS 被拦住或没有泄漏。
- 「系统」页显示 Geo 数据版本、规则数和运行信息。Geo 分类列表来自安装包里的 manifest，编辑器离线可用，写错的分类在保存或 Apply 前就会报错。

## 文件位置

```text
/etc/steer/config.json              0600，用户配置
/etc/steer/web.json                 0600，网页登录 token
/usr/share/steer/geodata-seed       只读的 SRS 数据和 manifest
/run/steer/current                  指向当前运行配置的链接
/run/steer/generations/<id>/        每次 Apply 生成的 intent、sing-box、platform、firewall
/run/steer/operation.lock           Apply、保存配置、订阅更新共用的锁
/run/steer/last-apply.json          最近一次 Apply 的结果
/var/lib/steer/cache.db             sing-box 的远端 SRS 和可选的 DNS 缓存
/var/lib/steer/subscriptions        订阅快照
```

Steer 不改 `/etc/resolv.conf`、NetworkManager 连接或 systemd-resolved 的配置片段；运行期间 systemd-resolved 的网卡 DNS 由 sing-box 自己管理。引导 DNS 只解析 DNS 服务器这类基础设施的域名，直连 UDP/TCP 引导 DNS 会有明文 53 查询，但里面没有用户访问的域名。

## 测试

发布前会在一次性的特权 systemd 容器里跑 `tests/integration/run-linux-system.sh`。它用两个独立的网络命名空间，测试本机和转发流量的 IPv4/IPv6 TCP、UDP、DNS（UDP/TCP 53）、监听端口的访问限制、禁用再启用、重启 `steer.service`、重启 `nftables.service` 后恢复。测试不改动开发机或 CI 机器本身的网络。
