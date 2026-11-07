# 配置参考

配置文件默认路径 `./config.yaml`；`--config PATH` 显式指定时文件必须存在。所有字段可省略，省略即使用内置默认值（与 `config.example.yaml` 一致，测试会校验两者同步）。

## telegram

| key | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `telegram.token` | string | `""` | @BotFather 提供的 token，`run` 模式必填 |
| `telegram.allowed_user_ids` | []int64 | `[]` | 允许使用机器人的用户 ID；`allow_all_users=false` 时必填 |
| `telegram.allow_all_users` | bool | `false` | 关闭白名单校验，仅限私有环境 |

## ehentai

| key | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `ehentai.cookie` | string | `""` | 登录 Cookie；原图必需，留空则使用重采样图 |
| `ehentai.user_agent` | string | Chrome 131 UA | 请求 User-Agent |
| `ehentai.proxy` | string | `""` | 代理地址，支持 `http://`、`https://`、`socks5://` |
| `ehentai.request_interval` | duration | `1s` | 两次请求的最小间隔，必须 >= 200ms |
| `ehentai.request_jitter` | duration | `300ms` | 在最小间隔上追加的随机抖动上限 |
| `ehentai.bandwidth_kib_per_sec` | int | `512` | 图片下载限速（KiB/s），0 表示不限速 |
| `ehentai.timeout` | duration | `60s` | 元数据/HTML 请求超时 |
| `ehentai.retries` | int | `3` | 网络错误、429、5xx 的重试次数 |
| `ehentai.prefer_original` | bool | `true` | 优先原图，失败回退重采样图 |
| `ehentai.max_pages` | int | `5000` | 单本画廊页数上限，超出直接报错 |

## download / telegraph

| key | 类型 | 默认值 | 说明 |
|---|---|---|---|
| `download.data_dir` | string | `./data` | 图片、元数据、证书、token 的根目录 |
| `telegraph.access_token` | string | `""` | 已有 telegra.ph token；留空时自动创建账号并保存到 data 目录 |
| `telegraph.author_name` | string | `telehentai` | 页面作者名 |
| `telegraph.author_url` | string | `""` | 页面作者链接 |
| `telegraph.max_content_bytes` | int | `49152` | 单页内容预算（字节），必须 >= 4096 |
| `telegraph.request_interval` | duration | `1s` | 两次 Telegraph 请求的间隔 |


## image_server

- `image_server.listen`（string，`:8088`）：监听地址；`tls.mode=autocert` 时固定监听 `:443`，此值被忽略。
- `image_server.public_base_url`（string，`""`）：图片对外地址，`run` 与 `publish` 必填；`run` 模式要求非回环地址必须使用 https，autocert 模式下 host 必须等于 `tls.domain`。
- `image_server.tls.mode`（string，`off`）：`off` 表示由反向代理终止 TLS；`autocert` 表示内置 Let's Encrypt。
- `image_server.tls.domain`（string，`""`）：autocert 需要的域名。
- `image_server.tls.email`（string，`""`）：autocert 的 ACME 注册邮箱。
- `image_server.tls.cache_dir`（string，`./data/autocert`）：证书缓存目录。
- `image_server.tls.http_listen`（string，`:80`）：autocert 模式下 HTTP 监听，用于 ACME 挑战与 302 跳转。

## pipeline / log

- `pipeline.workers`（int，`1`）：同时处理的画廊数量，必须 >= 1。
- `pipeline.queue_size`（int，`8`）：等待队列长度，必须 >= 1。
- `pipeline.progress_interval`（duration，`5s`）：Telegram 进度消息的最小编辑间隔。
- `pipeline.download_concurrency`（int，`4`）：单本画廊内并发下载的图片数，同时也是并发解析 `/s/` 页的数量。
- `log.level`（string，`info`）：`debug`/`info`/`warn`/`error`。
- `log.format`（string，`text`）：`text` 或 `json`。

## 环境变量

环境变量只在对应配置为空时生效，便于容器部署：

| 变量 | 覆盖字段 |
|---|---|
| `TELEHENTAI_TELEGRAM_TOKEN` | `telegram.token` |
| `TELEHENTAI_TELEGRAM_ALLOWED_USER_IDS` | `telegram.allowed_user_ids`（逗号分隔整数） |
| `TELEHENTAI_EHENTAI_COOKIE` | `ehentai.cookie` |
| `TELEHENTAI_TELEGRAPH_ACCESS_TOKEN` | `telegraph.access_token` |

## 获取 E-Hentai Cookie

1. 浏览器登录 `https://e-hentai.org/`（如需访问 exhentai，先登录表站再打开里站以生成 `igneous`）。
2. 打开开发者工具 → Application（Firefox 为「存储」）→ Cookies → `https://e-hentai.org`。
3. 复制 `ipb_member_id`、`ipb_pass_hash` 两项；访问 exhentai 还需要 `igneous`。
4. 拼成一行填入 `ehentai.cookie`：

```
ipb_member_id=123456; ipb_pass_hash=abcdef...; igneous=...
```

5. Cookie 等同账号凭据，不要提交到仓库（`config.yaml` 已在 `.gitignore` 中）。

没有 Cookie 时程序不会失败：`/fullimg` 会被重定向到登录页，随后自动回退到 H@H 重采样图，并在结果消息中提示。

程序会自动在 E-Hentai/ExHentai 请求中附加 `nw=1`，跳过「flagged as Offensive」警告页（该页没有缩略图容器）。如果你的 Cookie 里已经包含 `nw`，则保持原值。exhentai 的 Cookie 必须包含 `igneous`，否则站点会返回空页或 302 到 `?poni=no`。

## 校验规则

通用校验（所有模式）：`download.data_dir` 非空；`ehentai.request_interval >= 200ms`；`pipeline.workers >= 1`；`pipeline.queue_size >= 1`；`telegraph.max_content_bytes >= 4096`；`image_server.tls.mode` 只能是空、`off` 或 `autocert`。

- `run`：额外要求 `telegram.token`；未开启 `allow_all_users` 时要求白名单非空；`image_server.public_base_url` 必须是非空绝对 URL，且非回环地址必须 https；autocert 模式要求 `domain`、`email` 非空，且 URL 的 host 与 `domain` 相同。
- `publish`：要求 `image_server.public_base_url` 非空且为绝对 URL（`--image-base-url` 会在校验前覆盖它）。
- `fetch`、`serve-images`：无额外要求。
