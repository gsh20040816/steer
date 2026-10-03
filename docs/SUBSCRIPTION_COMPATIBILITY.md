# 分享链接兼容性

分享链接的导入和导出都只在 `go/internal/subscription` 里实现，结果再经过配置校验、存成 UCI/JSON、编译成 sing-box 配置。Linux 网页、LuCI 和 macOS 都没有自己的解析或拼接代码。

下表是目前支持的范围。

| 协议 | 输入格式 | 会保留的字段 | 限制 |
| --- | --- | --- | --- |
| SOCKS / SOCKS5 | `socks://`、`socks5://`，用户信息写在 userinfo | 用户名、密码、服务器、端口 | SOCKS5 统一存成 `socks`；不能带路径 |
| HTTP / HTTPS | `http://`、`https://`，用户信息写在 userinfo | 用户名、密码、TLS 名称、ALPN、uTLS、证书校验 | HTTPS 存成带 TLS 的 `http`；TLS 选项写在 query 里 |
| Shadowsocks | SIP002、userinfo Base64、旧式整段 Base64 | 加密方式、密码、服务器、端口、`plugin`/`plugin-opts` | 插件只支持 `obfs-local` 和 `v2ray-plugin`；未知 query 参数报错 |
| VMess | Base64 JSON（标准或 URL-safe，有无 padding 都行） | UUID、aid/scy、传输方式、Host/path、packet encoding、TLS SNI/uTLS/ALPN/证书校验 | 兼容 v2rayN 的写法：`v` 可以是数字，`alpn`/`fp` 可以是空字符串，不认识的字段直接忽略。设置了 socket network 的节点无法导出 |
| VLESS | `vless://` userinfo + query | UUID、TLS/Reality、uTLS、ALPN、传输方式、flow、packet encoding | `security` 只能是 `none`/`tls`/`reality`；别名冲突报错；允许空的 `pcs=` |
| Trojan | `trojan://` userinfo + query | 密码、TLS SNI/uTLS/ALPN/证书校验、传输方式 | 传输方式只支持 TCP/WS/gRPC/HTTP/QUIC；允许空的 `pcs=` |
| Hysteria | `hysteria://` userinfo + query | 认证、上下行带宽、TLS、uTLS、ALPN、obfs 密码、端口跳跃 | 上下行带宽必填；`obfs` 当作 obfs 密码 |
| Hysteria2 | `hysteria2://`、`hy2://` userinfo + query | 密码、TLS、uTLS、ALPN、Salamander、端口跳跃、带宽 | obfs 类型只支持 salamander |
| ShadowTLS | `shadowtls://` userinfo + query | 版本、密码、TLS SNI/uTLS/ALPN/证书校验 | 版本只能是 1–3；v2/v3 必须有密码 |
| TUIC | `tuic://` UUID[:密码] + query | UUID、可选密码、TLS SNI/uTLS/ALPN/证书校验、拥塞控制/UDP/心跳 | `allow_insecure`、`allowInsecure`、`insecure` 视为同一个参数；密码可以为空 |
| AnyTLS | `anytls://` userinfo + query | 密码、TLS SNI/uTLS/ALPN/证书校验 | `type` 只能是 tcp；允许空的 `pcs=` |
| NaiveProxy | `naive+https://` userinfo + query | 用户名、密码、TLS SNI/uTLS/ALPN/证书校验、QUIC、并发参数 | QUIC 和并发参数会校验 |
| SSH | `ssh://` userinfo | 用户名、密码、服务器、端口 | 私钥只能在结构化配置里填，不放进链接 |
| Tor | 没有分享链接 | 只能手动配置 | 界面上标为仅手动配置 |

## 通用规则

- 同一个参数出现多次时，值必须相同；已知的别名会统一，别名之间不能冲突。
- 布尔值写错、列表里有空项、有控制字符、ALPN 超过 255 字节、出现不认识的 URI 参数，都会报错。VMess 的 JSON 字段是例外，见上表。
- ALPN 用逗号分隔，存成列表。
- 关于 `pcs`：v2rayN/Xray 用它表示证书的 SHA-256，sing-box 的 `certificate_public_key_sha256` 算的是公钥哈希，两者没法互相转换。所以空的 `pcs=` 当作没设置，非空的直接报错。
- 订阅内容可以是逐行的链接（支持 CRLF），也可以是把同样内容整体 Base64 编码（标准或 URL-safe）。空行和 `#` 开头的行会跳过。
- 批量导入时，一条出错不影响其他条目。跳过的条目会在 `skipped_reasons` 里给出原因，不包含凭据和原始链接。

## 导出

节点列表里的「导出链接」会把当前草稿里的这个节点转成链接，并提醒链接里有完整的凭据。导出的链接必须能被同一个解析器重新导入，并且得到同样的出站配置；如果某个选项在标准格式里写不出来，导出直接失败，不会悄悄丢掉。Tor 没有分享链接；只用私钥认证、或者设置了 Host Key 限制的 SSH 节点，也不会把私钥或限制塞进链接。

各协议的样例和「导入 → 导出 → 再导入」测试在 `go/internal/subscription/subscription_test.go` 和 `export_test.go`。Canonical JSON、OpenWrt UCI 和编译器还有各自的回归测试，确保新字段不会在中间某一层丢掉。
