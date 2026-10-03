# 打包与发布

发布由三条流程组成：

- **Geo 数据流程**：定时把 Loyalsoldier 的 Geo 数据转成 SRS 并发布到 GitHub Pages。
- **CI**：每个分支提交和 PR 都在所有支持的平台上跑测试。
- **tag 发布流程**：推送 `v*` tag 后，从 tag 指向的源码构建、检查并发布 OpenWrt、Linux、macOS 的全部产物。

用户设备上不再安装 geoview，也不再保存或转换 DAT 文件。

## 基本规则

1. 本仓库发布：源码 tag、x86_64/aarch64 的 Linux tar.zst、OpenWrt 25.12.5 x86_64 的 APK、Apple Silicon 的 macOS DMG。不打 deb、rpm、pkg.tar 或 Nix 包。
2. Geo 流程每 6 小时检查一次 Loyalsoldier 的最新版本，用固定版本的 geoview 和 sing-box 把完整的 GeoSite/GeoIP 转成 SRS。Pages 上只保留最新的 `geodata/latest`。
3. 每份 Geo 数据都带一个清单（manifest），记录上游版本、DAT 文件的 SHA-256、转换工具的版本，以及每个分类对应 SRS 文件的路径、大小和 SHA-256。
4. 应用配置时只校验实际用到的 Geo 文件。sing-box 通过 `initial_path` 直接用本地文件启动，之后每 24 小时用直连去检查同名的远程 SRS 是否更新。
5. 安装包只依赖 `sing-box` 这个包名，不限版本。应用配置时，Steer 让实际安装的 sing-box 检查生成的配置并读取它的编译选项，不兼容就报错，让用户换合适的版本。CI、Geo 转换、OpenWrt 软件源里的 sing-box 和 macOS DMG 目前都固定用官方 `1.14.1`，但这不是 Linux/Arch 上的最低版本要求，能不能用以实际检查为准。
6. master 分支不构建、也不保存正式发布包。tag 必须指向 `origin/master` 上已有的提交；稳定版 tag 还要求同一个提交在 master 上的 CI 已经通过。GitHub Actions 出故障时，预发布版可以用完整的本地检查代替 master CI；tag 推送事件丢了的话，可以手动触发同一个 tag 的发布。两种方式都必须是 tag（`GITHUB_REF_TYPE=tag`），走完全相同的构建、检查、证明和发布流程。预发布不会覆盖稳定版的 OpenWrt 软件源。

当前稳定版是 `v0.11.5`。OpenWrt APK、Arch 的 `pkgver`、Git tag、Linux 和 macOS 产物都用 `0.11.5`。

## Geo 数据

`.github/workflows/geodata.yml` 是唯一的正式转换流程：

```text
Loyalsoldier 的 geosite.dat + geoip.dat
                  ↓
固定版本的 geoview + 固定版本的 sing-box
                  ↓
完整的 rules/*.srs + manifest.json
                  ↓
逐个文件反编译、检查非空、核对大小和 SHA-256
                  ↓
GitHub Pages：geodata/latest
```

geoview 只是 CI 里转换工具的构建依赖，Steer 和用户设备都不需要它。上游的 tag 名会先按格式校验，再交给后续步骤使用。

manifest 是可用分类的唯一清单，包括基础分类和 `category@attribute` 形式的属性分类。上游删掉的分类，下次应用配置时会直接报错，不会悄悄换成别的。

公开地址：

```text
https://gsh20040816.github.io/steer/geodata/latest/manifest.json
https://gsh20040816.github.io/steer/geodata/latest/rules/steer-geosite-cn.srs
https://gsh20040816.github.io/steer/geodata/latest/steer-geodata.tar.zst
```

Pages 只放当前版本，不保留历史版本。tag 发布流程先下载并完整校验当前的数据包，再把同一份数据放进这次的 OpenWrt、Linux 和 macOS 产物，并在构建信息里记录它的版本和 manifest 的 SHA-256。

## OpenWrt

`v0.11.5` 面向 OpenWrt 25.12.5 x86/64：

| 包 | 版本 | 内容 |
|---|---|---|
| `steer` | `0.11.5-r1` | 控制程序、默认 UCI 配置、procd 启动脚本、完整的只读 Geo 数据 |
| `luci-app-steer` | `0.11.5-r1` | LuCI 页面、ucode RPC、权限配置 |
| `luci-i18n-steer-zh-cn` | `0.11.5-r1` | 简体中文翻译 |
| `sing-box` | `1.14.1-r0` | SagerNet 官方 x86_64 APK，核对内容后改用 Steer 的密钥签名 |

Steer 不重新编译 sing-box。CI 先核对官方 APK 的 SHA-256，重签后比较除签名外的元数据是否一致，再用仓库公钥验证。软件源里的四个 APK 和 `packages.adb` 都用同一把 Steer P-256 密钥签名。私钥只存在 GitHub Actions 的 Secret `OPENWRT_APK_PRIVATE_KEY` 里，不能出现在源码、构建产物、Release 或 Pages 上。

`steer` 包依赖 `firewall4`、`ip`、`kmod-tun`、`kmod-nft-queue`、`kmod-nft-tproxy` 和 `sing-box`（不限版本），不依赖 geoview，也没有单独的 Geo 包。

文件位置：

```text
/etc/config/steer                    用户的 UCI 配置（升级时保留）
/usr/share/steer/geodata-seed        随包安装的只读 Geo 数据和清单
/var/lib/steer/cache.db              sing-box 下载的远程 SRS 和可选的 DNS 缓存
/var/lib/steer/subscriptions         订阅快照
/var/lib/steer/logs/tests            最近的测试结果
/run/steer                           运行配置、当前配置链接、最近一次应用记录、锁
```

安装后脚本会设置订阅的 cron 任务，然后正常运行一次 `steer apply`。只支持 schema 9，旧格式的配置会直接报错，没有版本迁移命令。包里仍然声明了 `PROVIDES`、`CONFLICTS` 和 `REPLACES`，用来替换旧包名 `steer-openwrt`，但源码里没有任何兼容逻辑。

### 稳定版软件源

只有稳定版 tag 发布成功后，`release.yml` 才会把这次构建出的四个 APK、签名索引、公钥和校验信息部署到 GitHub Pages。预发布不会覆盖这里。Geo 流程和发布流程共用 `pages-site` 互斥组，各自只更新自己的目录，不影响对方。

```text
https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/packages.adb
https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/steer-apk.pem
```

OpenWrt 上先装公钥，再添加软件源：

```sh
wget -O /etc/apk/keys/steer-apk.pem \
  https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/steer-apk.pem
echo 'https://gsh20040816.github.io/steer/openwrt/25.12.5/x86_64/packages.adb' \
  >> /etc/apk/repositories.d/customfeeds.list
apk update
apk add steer luci-app-steer luci-i18n-steer-zh-cn
```

不要用 `--allow-untrusted` 代替安装公钥。如果要更换私钥，必须用新的公钥文件名发布，并先让已有设备信任新公钥。

## 通用 Linux

GitHub Release 提供：

```text
steer-linux-x86_64.tar.zst
steer-linux-aarch64.tar.zst
```

压缩包的结构：

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

压缩包里没有 sing-box、geoview 或 DAT 文件。目标系统需要自己装好 systemd、合适版本的 sing-box、nftables、iproute2 和 ca-certificates。发行版维护者请从固定的源码 tag 构建 `./go/cmd/steer-linux`，安装为 `/usr/bin/steer`，并原样安装校验过的 Geo 数据。

## macOS DMG

GitHub Release 只提供 Apple Silicon 版：

```text
steer-macos-arm64.dmg       # xcode-27 / arm64 / Xcode 27.0
```

Swift 界面不交叉编译。构建任务会编译 Swift 程序和同架构的 `steer-macos`，下载 SagerNet 官方 `sing-box 1.14.1` 的 macOS 压缩包并核对固定的 SHA，然后调用 `macos/scripts/build-app-bundle.sh` 组装。DMG 里的内容固定为：

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

Swift 的测试、构建和查询输出路径都显式加上 `--build-system native`。原因是 Swift 6.4 默认的构建系统在当前工具链下，会把二进制里记录的 SDK 版本写成最低部署版本 13.0；原生构建系统能正确记录 SDK 27.0。检查时同时要求最低部署版本是 13.0、SDK 是 27。

组装时 `Info.plist` 会写入真实版本号、纯数字的 build number、`CFBundleExecutable=SteerApp`、`CFBundleIdentifier=com.steer.steer` 和 `LSMinimumSystemVersion=13.0`，不能留下 Xcode 的变量占位。构建会检查：SDK 和最低部署版本、二进制只有一种架构、文件权限、`steer-macos` 的校验和节点解析能正常运行、sing-box 的版本/编译选项/提交、Geo 清单、没有混进不该有的文件。签名时先 ad-hoc 签内部程序，最后签整个应用。

项目没有 Developer ID，构建信息里写的是 `Notarization: none`。DMG 和应用都不能宣称已公证；第一次打开时需要手动确认，这是预期行为。

应用内的安装器和卸载器只读取 `Resources/Installer` 里的文件，这些文件必须是普通文件（不能是符号链接）并且和 SHA 清单一致；不依赖 PATH，也不在用户电脑上编译。第一次安装时输入一次管理员密码，装好 root 所有的后台程序和 sing-box，以及三个 LaunchDaemon（运行、control、订阅定时）。日常的保存、应用、测试、订阅更新都通过 `/var/run/steer/control.sock` 交给 control 服务完成，它会核对调用者的身份，所以不需要再输入密码。修复和默认卸载都保留用户的配置、状态和日志；要删用户数据，需要单独再确认一次。

DMG 和最终的 `SHA256SUMS` 附有 GitHub 构件证明，可以这样验证：

```sh
gh attestation verify steer-macos-arm64.dmg -R gsh20040816/steer
```

构件证明只说明文件来自对应的 GitHub 构建流程，不能代替 Developer ID、公证，也不会让 Gatekeeper 放行。

macOS 的原始安装包只从 GitHub Release 获取。以后如果做 Homebrew Cask，也只是在第一个稳定版 DMG 发布后，引用 Release 的下载地址和准确的 SHA；Cask 本身不负责编译。在那之前不写 Cask。

## Arch Linux（AUR）

本仓库只维护一份源码打包配方：

```text
packaging/archlinux/steer/
├── PKGBUILD
├── .SRCINFO
└── .gitignore
```

配方从固定的 `_commit` 构建 Steer，并下载 Pages 上当前的 Geo 数据包。`prepare()` 每次都先删掉旧的 `$srcdir/geodata-seed`，再解压并用同一个 Go 校验工具完整检查，所以重复运行 `makepkg` 不会失败，也不会混进旧数据。

依赖里写的是虚拟包名 `sing-box`，这样官方包和 Arch Linux CN 的 `sing-box-alpha` 都能用（后者提供了 `sing-box` 这个名字，但没有提供能让 pacman 比较版本的版本号）。sing-box 能不能用，由 Steer 在应用配置时实际检查。其他依赖是 systemd、nftables、iproute2 和 ca-certificates。安装时不做配置迁移、不启用服务、不应用配置，也不生成网页 token。

`PKGBUILD` 是唯一需要手写的文件，`.SRCINFO` 必须用 `makepkg --printsrcinfo` 生成（`check-release-consistency.py` 会检查两者是否一致）。更新步骤：

1. 改 `pkgver` 和 40 位的 `_commit`；如果只是打包方式变了，只加 `pkgrel`。
2. 重新生成 `.SRCINFO`。
3. 在干净的 Arch 环境里运行 `makepkg --cleanbuild --syncdeps`，检查文件、权限、版本、Geo 数据和依赖。
4. 人工复制并检查包目录，然后推送到单独的 AUR 仓库。CI 里不保存 AUR 的凭据。

## 构建和发布流程

```text
每个提交 / PR 的 CI（不限并发）
  ├── Go 测试（-race）+ vet
  ├── Node/LuCI 测试
  ├── 翻译、版本一致性、界面描述文件检查
  ├── 各平台命令的编译检查
  ├── Linux systemd 容器集成测试
  └── arm64 macOS 上的 Go + Swift 测试和编译
        ↓ tag 检查（提交在 master 上、版本号一致；稳定版还要同一提交的 CI 通过）
v* tag 发布流程
        ↓
下载并校验 Pages 上的 Geo 数据
  ├── OpenWrt SDK 构建 Steer/LuCI/翻译三个 APK
  │     + 核对并重签官方 sing-box APK
  │     + 签名 packages.adb
  ├── CGO_ENABLED=0 构建两个 Linux tar.zst
  └── arm64 机器上构建 macOS DMG
        ↓
各平台产物检查
        ↓
汇总本次构建的产物，生成统一的构建信息和校验和，生成构件证明
        ↓
发布 GitHub Release；只有稳定版 tag 更新 Pages 上的 OpenWrt 软件源
```

本地完整检查：

```sh
cd go
go test -race ./...
go vet ./...
cd ..
python3 tests/check-luci-i18n.py
python3 tests/check-release-consistency.py
python3 tests/check-ui-contract.py
python3 tests/check-macos-packaging.py
node tests/node/luci_view_test.js
node tests/node/steer_helper_test.js
node tests/node/linux_web_test.js
sh -n tests/integration/run-openwrt-vm.sh
sh -n tests/integration/run-linux-system.sh
git diff --check
```

发版步骤：

1. 把改动推到 master。稳定版等对应的 CI 通过；预发布版如果碰上 Actions 故障，记下本地完整检查的结果。
2. 给同一个提交打版本 tag 并推送。如果 Actions 没有因为 tag 推送启动构建，手动触发这个 tag。
3. `release.yml` 检查提交是否在 master 上、tag 和源码里的版本号是否一致；稳定版还检查 master CI。然后从 tag 源码构建所有平台。
4. 汇总任务只下载本次构建的 OpenWrt、Linux、macOS 产物，核对各自的校验和与源码提交，然后生成统一的构建信息和校验和。
5. 构件证明完成后创建 GitHub Release。只有稳定版 tag 会更新 Pages 上的 OpenWrt 软件源；预发布只创建 prerelease。

最终的 Release 包含四个 APK、两个 Linux tar.zst、一个 macOS arm64 DMG、`BUILD-METADATA.txt` 和 `SHA256SUMS`。发布任务不会下载 master 或其他构建的正式产物。
