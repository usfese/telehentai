# 架构决策记录（ADR）

每条记录包含背景、决策与后果。编号顺序即决策时间顺序。

## ADR-001：自建图床，而不是用 Telegraph 上传

**背景**：Telegraph 新媒体上传接口已于 2024-09-06 被官方关闭（Durov 公告）；实测 `telegra.ph/upload` 与 `graph.org/upload` 对合法 JPEG/PNG 均返回 `400 Unknown error`。而 `createPage` 接受外部图片 URL，能正常渲染 `<img src="...">`（实测 500 张外链图的页面可正常返回）。

**决策**：图片保存在本地，由内置 `imgsrv` 以 `/i/<gid>/<页码>.<ext>` 暴露；telegra.ph 只存文字与图片外链。

**后果**：部署者必须提供公网 HTTPS 域名（或反代）；页面可用性依赖自建图床；换来的是不依赖任何已关闭的官方上传能力，且图片完全可控。

## ADR-002：优先原图，失败自动回退重采样图

**背景**：原图链接 `/fullimg/<gid>/<page>/<key>/<name>` 在无 Cookie 时 302 到 `bounce_login.php`（登录页）；无 Cookie 时 `img#img` 的 H@H 地址（`*.hath.network/h/...keystamp=...`）可直接返回 webp/jpg。`fullimg` 到文件服务器的具体跳转链无法在无 Cookie 环境完整实测。

**决策**：`Open` 依次尝试「原图（prefer_original 为真且存在）→ 重采样图」；把非图片响应（登录页 HTML）识别为 `ErrAuthRequired` 并继续下一候选；结果里用 `UsedFallback` 标记是否降级，并提示用户。

**后果**：没有 Cookie 也能完整跑通（这是常态路径，不是异常）；有 Cookie 时优先原图；代价是每页可能多一次被重定向的请求，且用户会看到「已使用重采样图」的提示。

## ADR-003：逐页下载，而不是 `/archiver.php`

**背景**：E-Hentai 的整本打包下载走 `archiver.php`，需要 GP 点数且队列不可靠，失败模式多（等待、限流、链接过期）。

**决策**：从缩略图页逐页解析 `/s/` 链接，再逐页解析图片地址并下载；已存在的页跳过，失败页重试一次后计入 `Failed` 并继续。

**后果**：请求数量多，但每一步都可观测、可续传、可限速；单页失败不会拖垮整本。

## ADR-004：内容分页预算 48 KiB，超限按 0.6 倍回退

**背景**：telegra.ph 对单页 content JSON 有硬上限。实测 500 个图片节点（58.6 KB）成功，600 个（70.3 KB）返回 `CONTENT_TOO_BIG`；具体阈值会变化。

**决策**：默认预算 49152 字节，`BuildPart` 按节点 JSON 字节数累加，并为导航节点预留 512 字节；若 `createPage` 仍返回超限，把预算乘 0.6 重试同一页（低于 4096 则放弃）。

**后果**：长画廊自动切成多页，页面间用「上一部分/下一部分」串联；发布长画廊会多次调用 createPage/editPage，受 `telegraph.request_interval` 节流。

## ADR-005：请求节流加字节限速

**背景**：E-Hentai 对高频请求敏感（429、403、临时封禁），图片 CDN 也会限速。

**决策**：`Pacer` 保证任意两次请求间隔不小于 `request_interval`（默认 1s）并叠加 0..300ms 抖动；图片读取经 `rate.Limiter` 按 `bandwidth_kib_per_sec`（默认 512 KiB/s）限速；429 优先遵循 `Retry-After`。

**后果**：下载一本 20 页画廊约需 1 分钟量级；换来的是长期稳定、不触发风控，且可通过配置按部署环境调整。

## ADR-006：长轮询，而不是 webhook

**背景**：webhook 需要公网 HTTPS、证书与回调校验，部署门槛高；本项目图床本身就需要公网暴露，但机器人不应依赖同一域名。

**决策**：使用 go-telegram/bot 的长轮询（`Start(ctx)`），无需公网回调地址。

**后果**：部署简单（内网也能跑机器人），代价是空闲时有轮询开销；多实例部署会互相抢更新，本项目按单实例设计。

## ADR-007：用户白名单

**背景**：机器人会下载并公网发布内容，若对所有人开放，等于把图床变成公开代理。

**决策**：默认只服务 `telegram.allowed_user_ids` 中的用户；`allow_all_users` 仅用于自建私有环境；未授权用户会收到包含自己 ID 的提示，方便配置。

**后果**：配置多一步（填 ID），但避免资源被滥用；`run` 模式校验时要求 token 与白名单同时有效。

## ADR-008：内置 autocert（默认推荐）

**背景**：telegra.ph 页面要求图片是 HTTPS；手工申请续期证书容易出错。

**决策**：`tls.mode=autocert` 时使用 `golang.org/x/crypto/acme/autocert`，固定监听 :443，HTTP 监听负责 ACME 挑战与 302 跳转；同时保留 `tls.mode=off` 供反向代理使用。

**后果**：单机部署开箱即用；需要 80/443 可达且域名解析正确。若位于 CDN/反代之后，应改用 `off` 并让反代终止 TLS。

## ADR-009：模块声明 Go 1.26

**背景**：计划最初假设 `go 1.25`，但固定的依赖版本 `golang.org/x/net v0.62.0` 与 `golang.org/x/time v0.16.0` 的 `go` 指令均为 `1.26.0`。

**决策**：`go.mod` 声明 `go 1.26.0`，CI 与 Dockerfile 同步使用 Go 1.26。依赖版本保持计划中固定的版本。

**后果**：构建环境需要 Go 1.26 及以上；换来的是使用较新的 `x/net/html` 与 `x/time/rate`，并避免因降级依赖丢失修复。




