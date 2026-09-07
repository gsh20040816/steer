# 打包与发布

Steer 采用三条相互校验的交付链：定时 Geo 工作流发布当前 SRS；所有分支 commit 与 PR 在全部支持平台运行测试；`v*` tag workflow 从 tag 指向的源码构建、验收并发布所有 OpenWrt、Linux 和 macOS 构件。目标设备不再安装 geoview，也不再持有或转换 DAT。

## 共同规则

1. 主仓库发布 source tag、x86_64/aarch64 Linux tar.zst、OpenWrt 25.12.5 x86_64 APK，以及 arm64/x86_64 原生 macOS DMG；不构建 deb、rpm、pkg.tar 或 Nix 包。
2. Geo 工作流每 6 小时检查 Loyalsoldier 最新 release，以固定版本的生成器和 sing-box 把完整 GeoSite/GeoIP 转换成 SRS，并只保留 Pages 上的 `geodata/latest`。
3. 每份 seed 都有严格 manifest，记录上游版本、DAT SHA-256、转换工具版本以及每个 selector 对应 SRS 的路径、大小和 SHA-256。
4. 设备 Apply 只校验所引用的 seed 文件；sing-box 通过 `initial_path` 立即启动，并使用 direct HTTP client 每 24 小时后台检查同名 remote SRS。
5. 控制器只依赖无版本的 `sing-box` 提供者；Apply 通过实际二进制的 native config check 和 build tags 判断能力，不满足时明确要求用户指定兼容版本/构建。当前 CI/Geo 验证、OpenWrt 软件源镜像和 macOS DMG 固定使用官方 `1.14.0`；这不构成 Linux/Arch 的最低运行时版本约束，兼容构建仍由实际能力检查裁决。
6. master 不构建或保存正式发布包。tag 必须指向 `origin/master` 的祖先；稳定 tag 还要求同一 `head_sha` 已有成功的 master CI push run。GitHub Actions 服务降级时，预发布允许以完整本地发布门替代独立 master CI；tag push 事件丢失时可显式 dispatch 同一版本 tag。两种入口都要求 `GITHUB_REF_TYPE=tag`，并运行完全相同的构建、验收、attest 和发布链。预发布不替换稳定 OpenWrt 软件源。

当前稳定版本是 `v0.10.1`；OpenWrt APK、Arch `pkgver`、Git tag、Linux 与 macOS 构件均使用 `0.10.1`。

## Geo SRS

`.github/workflows/geodata.yml` 是唯一生产转换链：

```text
Loyalsoldier geosite.dat + geoip.dat
                  ↓
固定 geoview library + 固定 sing-box compiler
                  ↓
完整 rules/*.srs + manifest.json
                  ↓
逐文件反编译、非空检查、大小与 SHA-256 验证
                  ↓
GitHub Pages geodata/latest
```

这里的 geoview 仅是 CI 生成器的 Go 构建依赖，不是 Steer 或目标设备的运行时依赖。manifest 是 selector 的唯一清单，包含基础 category 及 `category@attribute`；已从上游删除的 selector 会在下一次 Apply 明确失败，不做静默 fallback。

公开地址：

```text
https://gsh20040816.github.io/steer/geodata/latest/manifest.json
https://gsh20040816.github.io/steer/geodata/latest/rules/steer-geosite-cn.srs
https://gsh20040816.github.io/steer/geodata/latest/steer-geodata.tar.zst
```

Pages 只保存当前版本，不承诺历史 seed 或可重复取得任意旧上游输入。tag workflow 先下载、展开并完整验证一个确定的当前 bundle，再把同一 seed 嵌入该次 OpenWrt、Linux 和 macOS 产物；构件元数据记录其版本和 manifest SHA-256。

## OpenWrt

`v0.10.1` 面向 OpenWrt 25.12.5 x86/64：

| 包 | 版本 | 所有内容 |
|---|---|---|
| `steer` | `0.10.1-r1` | 控制器、默认 UCI、procd init、完整只读 SRS seed |
| `luci-app-steer` | `0.10.1-r1` | LuCI 页面、ucode RPC、ACL |
| `luci-i18n-steer-zh-cn` | `0.10.1-r1` | 简体中文翻译 |
| `sing-box` | `1.14.0-r0` | SagerNet 官方 x86_64 APK，经内容核验后改用 Steer 仓库密钥签名 |

Steer 不重编译 sing-box。CI 先校验官方 APK 的固定 SHA-256，重签后比较除签名记录外的 APK 元数据，再用仓库公钥验证安装包。软件源的四个 APK 与 `packages.adb` 使用同一 Steer P-256 信任根；私钥只来自 GitHub Actions Secret `OPENWRT_APK_PRIVATE_KEY`，不得进入源码、构件、Release 或 Pages。

控制器依赖 `firewall4`、`ip`、`kmod-tun`、`kmod-nft-queue`、`kmod-nft-tproxy` 和上述 sing-box 版本范围，不依赖 geoview 或独立 Geo 包。

持久与易失状态：

```text
/etc/config/steer                    用户 UCI；包 conffile
/usr/share/steer/geodata-seed        包拥有的只读 SRS seed 与 manifest
/var/lib/steer/cache.db              sing-box remote SRS 与可选 DNS cache
/var/lib/steer/subscriptions         订阅快照
/var/lib/steer/logs/tests            最新测试报告
/run/steer                           generation、current、last_apply、锁
```

安装后脚本维护订阅 cron，再运行正常 `steer apply`。正式版只接受 schema 9，旧 schema 配置会立即失败，不再携带版本迁移命令或安装 hook。包仍通过 `PROVIDES`、`CONFLICTS` 和 `REPLACES` 原子替换历史 `steer-openwrt` 包名，但源码中不保留兼容实现。

### 稳定软件源

稳定 tag 发布成功后，`release.yml` 才把同一次 tag run 产生的四个 APK、签名索引、公钥和校验元数据部署到 GitHub Pages。预发布不会替换这里的 OpenWrt 内容；独立 Geo 工作流与发布 workflow 共享 `pages-site` 互斥组，保留其他子树并只更新各自目录。

```text
https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/packages.adb
https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/steer-apk.pem
```

OpenWrt 端先安装公钥，再添加索引：

```sh
wget -O /etc/apk/keys/steer-apk.pem \
  https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/steer-apk.pem
echo 'https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/packages.adb' \
  >> /etc/apk/repositories.d/customfeeds.list
apk update
apk add steer luci-app-steer luci-i18n-steer-zh-cn
```

不得以 `--allow-untrusted` 代替密钥安装。轮换私钥时必须发布新文件名，并先完成设备信任迁移。

## 通用 Linux

GitHub Release 提供：

```text
steer-linux-x86_64.tar.zst
steer-linux-aarch64.tar.zst
```

每个归档结构固定为：

```text
steer-linux-<arch>/
├── steer
├── systemd/
│   ├── steer.service
│   ├── steer-web.service
│   ├── steer-subscription.service
│   └── steer-subscription.timer
├── config.example.json
├── web.example.json
├── geodata-seed/
│   ├── manifest.json
│   └── rules/*.srs
└── LICENSE
```

归档不包含 sing-box、geoview 或 DAT。目标系统必须提供 systemd、匹配版本的 sing-box、nftables、iproute2 和 ca-certificates。发行版维护者从固定 source tag 构建 `./go/cmd/steer-linux`，安装为 `/usr/bin/steer`，并原样安装已经验证的 seed。

## macOS DMG

GitHub Release 提供两个由对应原生 runner 构建的 DMG：

```text
steer-macos-arm64.dmg       # macos-26 / arm64 / Xcode 26.6
steer-macos-x86_64.dmg      # macos-26-intel / x86_64 / Xcode 26.6
```

Swift GUI 不交叉编译。每个 job 构建 release Swift package 和同架构 `steer-macos`，下载 SagerNet 官方 `sing-box 1.14.0` Darwin archive并严格校验固定 SHA，然后调用唯一的 `macos/scripts/build-app-bundle.sh`。DMG 内固定包含：

```text
Steer.app/
└── Contents/
    ├── MacOS/SteerApp
    ├── Info.plist
    └── Resources/
        ├── Installer/
        │   ├── steer-macos
        │   ├── sing-box
        │   ├── install-embedded-payload.sh
        │   ├── uninstall-embedded-payload.sh
        │   ├── com.steer.steer.plist
        │   ├── com.steer.steer.control.plist
        │   ├── com.steer.steer.subscription.plist
        │   ├── config.example.json
        │   └── PAYLOAD-SHA256SUMS
        ├── geodata-seed/{manifest.json,rules/...}
        └── LICENSES/{Steer-GPL-3.0.txt,sing-box-GPL-3.0.txt}
```

`Info.plist` 在组装时写入真实版本、纯数字 build number、`CFBundleExecutable=SteerApp`、`CFBundleIdentifier=com.steer.steer` 与 `LSMinimumSystemVersion=13.0`，不得残留 Xcode build setting。Swift GUI 固定由 Xcode 26.6/macOS 26 SDK 构建，同时保持最低部署目标 13.0；构建会检查 SDK、deployment target、Mach-O 单一架构、权限、helper validate/parse-nodes、sing-box version/tags/revision、Geo manifest、禁止文件，并先 ad-hoc 签嵌套二进制再签 App。项目没有 Developer ID，`Notarization: none`；DMG 与 App 不得宣传为 notarized，Gatekeeper 首次手动确认属于预期行为。

正式 App 的 embedded installer/受控 uninstaller 只从 `Bundle.resources/Installer` 读取普通、非 symlink、带 SHA 清单的 payload，不依赖 PATH，也不现场编译。首次安装输入一次管理员密码，安装 root-owned helper/sing-box、运行数据面、control 与订阅调度三个 LaunchDaemon。control 只在 `/var/run/steer/control.sock` 接受经过 peer credential 校验的 `save`、`apply` 和订阅更新/清理请求，因此日常 GUI 写操作不再重复授权；Repair 与默认卸载保留用户 config/state/logs，删除用户数据必须独立二次确认。

DMG 与最终 `SHA256SUMS` 使用 GitHub artifact attestation。验证示例：

```sh
gh attestation verify steer-macos-arm64.dmg -R gsh20040816/steer
```

attestation 证明构件来自对应 GitHub workflow，不替代 Developer ID、公证或 Gatekeeper 放行。

GitHub Release 是 macOS 原始构件的唯一来源。Homebrew Cask 只能在首个稳定 DMG 发布后作为下游索引，引用 Release DMG URL 与精确 SHA；Cask 不承担编译，稳定 DMG 之前不手写或发布配方。

## Arch Linux AUR

本仓库只维护一个源码配方：

```text
packaging/archlinux/steer/
├── PKGBUILD
├── .SRCINFO
└── .gitignore
```

配方从固定 `_commit` 构建 Steer，并下载 Pages 当前 Geo bundle；`prepare()` 每次先清理旧的 `$srcdir/geodata-seed`，再展开并用同一 Go verifier 完整校验 seed，因此重复运行 `makepkg` 不会因目录已存在而失败，也不会混入旧 seed。Arch 依赖声明使用虚拟包名 `sing-box`，以兼容官方包和 Arch Linux CN 的 `sing-box-alpha`（后者提供 `sing-box` 但不提供可用于 pacman 版本比较的 versioned provides）；Steer 在 Apply 时通过实际二进制的 native config check 和 build tags 判断能力，不满足时要求指定兼容版本/构建。其他运行依赖是 systemd、nftables、iproute2 和 ca-certificates。正式版没有配置迁移 install hook，不启用服务、不 Apply 配置，也不生成 Web token。

`PKGBUILD` 是唯一手工维护的配方，`.SRCINFO` 必须由 `makepkg --printsrcinfo` 生成。更新时：

1. 更新 `pkgver` 和 40 位 `_commit`；仅打包修订才只增加 `pkgrel`。
2. 重新生成 `.SRCINFO` 并确认与 `PKGBUILD` 一致。
3. 在干净 Arch 环境运行 `makepkg --cleanbuild --syncdeps`，检查文件、权限、版本、seed 和依赖。
4. 人工复制并审查包目录后，再推送独立 AUR Git 仓库；CI 不保存 AUR 凭据。

## 主线构建与发布门

```text
all-commit / PR CI（无 concurrency 限制）
  ├── Go race tests + vet
  ├── Node/LuCI tests
  ├── i18n、包边界、workflow/Linux/macOS 打包契约
  ├── Go 平台命令 smoke build
  ├── Linux systemd 容器集成测试
  └── arm64/x86_64 原生 Go + Swift debug/release 测试
        ↓ tag source gate（master 祖先 + 版本一致；稳定版另需同 SHA 成功 CI）
v* tag workflow
        ↓
verified Pages Geo seed
  ├── OpenWrt SDK 构建三个 Steer/LuCI APK
  │     + 校验并重签官方 sing-box APK
  │     + 签名 packages.adb
  ├── CGO_ENABLED=0 构建两份 Linux tar.zst
  └── 原生 runner 构建两个 macOS DMG
        ↓
macOS bundle/DMG 与各平台产物验收
        ↓
本次 run verified release bundle + attestation
        ↓
GitHub Release；仅稳定 tag 更新 Pages OpenWrt
```

本地完整验证至少包括：

```sh
cd go
go test -race ./...
go vet ./...
cd ..
python3 tests/check-luci-i18n.py
python3 tests/check-package-boundaries.py
python3 tests/check-build-cache.py
python3 tests/check-linux-packaging.py
python3 tests/check-macos-packaging.py
node tests/node/luci_view_test.js
node tests/node/steer_helper_test.js
node tests/node/linux_web_test.js
python3 tests/check-ui-contract.py
sh -n tests/integration/run-openwrt-vm.sh
sh -n tests/integration/run-linux-system.sh
git diff --check
```

tag 流程：

1. 推送 master 原子变更；稳定版等待对应 `CI` push run 成功，预发布在 Actions 降级时记录完整本地发布门结果。
2. 给同一 commit 打版本 tag 并推送；若 Actions 未登记 tag push run，显式 dispatch 该 tag。
3. `release.yml` 检查 ancestry 和 tag/源码版本一致性；稳定版还检查同 SHA master CI，再从 tag 源码构建全部平台。
4. bundle job 只下载本次 run 的 OpenWrt/Linux/macOS artifacts，逐层校验后生成统一元数据和校验和。
5. attestation 完成后创建 GitHub Release；稳定 tag 才更新 Pages OpenWrt 软件源，预发布只创建 prerelease。

最终 Release 包含四个 APK、两个 Linux tar.zst、两个 macOS DMG、`BUILD-METADATA.txt` 和 `SHA256SUMS`。publish job 不得下载 master 或其他 run 的正式构件。
