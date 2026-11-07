# telehentai

把 e-hentai / exhentai 画廊变成 telegra.ph 图集：Telegram 里发链接，机器人下载整本画廊、托管到自己对外暴露的 HTTPS 图片服务器，并按顺序发布为 telegra.ph 文章。

## 为什么需要自建图床

Telegraph 的**新媒体上传接口已于 2024-09-06 被官方关闭**（Durov 公告），`telegra.ph/upload` 对合法 JPEG/PNG 也返回 `400 Unknown error`。telegra.ph 仍然接受**外部图片 URL**，并会渲染为 `<img src="...">`。

所以本项目的做法是：

1. 图片保存在本地 `data/`，由内置图床以稳定 URL（`/i/<gid>/<页码>.<ext>`）提供；
2. 只用 telegra.ph 的 `createPage` / `editPage` 存文字与图片外链。

这带来三个硬约束：

| 约束 | 原因 | 项目中的处理 |
|---|---|---|
| 图床必须 HTTPS（`run` 模式） | telegra.ph 是 HTTPS 页面，浏览器会拦截/升级 `http://` 图片 | 配置校验拒绝非回环地址的 http；内置 autocert 或反代二选一 |
| 原图需要登录 Cookie | `/fullimg/...` 在无 Cookie 时 302 到 `bounce_login.php` | 自动回退 `img#img` 的重采样图，并在结果中提示 |
| 单页内容有体积上限 | 实测 500 个图片节点（58.6 KB）成功，600 个（70.3 KB）返回 `CONTENT_TOO_BIG` | 每页预算 48 KiB，超限按 0.6 倍回退重试 |

## 快速开始

```bash
# 1. 依赖：Go 1.26+（golang.org/x/net v0.62 的 go 指令要求）
cp config.example.yaml config.yaml

# 2. 至少填写：
#    telegram.token               @BotFather 给的 token
#    telegram.allowed_user_ids    允许使用机器人的 Telegram 用户 ID
#    ehentai.cookie               E-Hentai 登录 Cookie（想要原图时必填）
#    image_server.public_base_url 图床公网地址，例如 https://img.example.com

# 3. 启动（图床 + 机器人）
go run ./cmd/telehentai run
```

然后在 Telegram 里给机器人发画廊链接，例如
`https://e-hentai.org/g/4310985/ad9552b3e4/`，机器人会回复进度，最终给出 telegra.ph 链接。

敏感字段也可以用环境变量提供：`TELEHENTAI_TELEGRAM_TOKEN`、`TELEHENTAI_EHENTAI_COOKIE`、`TELEHENTAI_TELEGRAPH_ACCESS_TOKEN`、`TELEHENTAI_TELEGRAM_ALLOWED_USER_IDS`（逗号分隔）。环境变量只在对应配置为空时生效。

### 命令行

```bash
telehentai run          [--config PATH]                      # 图床 + Telegram 机器人
telehentai fetch        [--config PATH] [--data-dir DIR] [--limit N] <链接>
telehentai publish      [--config PATH] [--data-dir DIR] [--image-base-url URL] [--limit N] <gid|链接>
telehentai serve-images [--config PATH] [--listen ADDR] [--data-dir DIR]
telehentai version
```

注意：flag 必须写在位置参数之前（标准库 `flag` 的解析规则）。

`fetch` + `serve-images` + `publish` 三个子命令可以在**没有 Telegram token、没有 Cookie** 的情况下完成一次端到端冒烟：

```bash
telehentai fetch --limit 3 --data-dir /tmp/thdata "https://e-hentai.org/g/4310985/ad9552b3e4/"
telehentai serve-images --data-dir /tmp/thdata --listen 127.0.0.1:18088 &
telehentai publish --data-dir /tmp/thdata --image-base-url http://127.0.0.1:18088 4310985
```

## Docker

```bash
docker build -t telehentai .

docker run -d --name telehentai \
  -p 443:443 -p 80:80 \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -v "$PWD/data:/data" \
  telehentai
```

镜像里 `ENTRYPOINT` 固定为 `run --config /config/config.yaml`。要跑其他子命令，覆盖 entrypoint，例如
`docker run --rm -v "$PWD/data:/data" telehentai fetch --data-dir /data <链接>`。

## 部署 HTTPS

### 方式一：内置 autocert（推荐）

```yaml
image_server:
  listen: ":8088"            # autocert 模式下忽略，固定监听 :443
  public_base_url: "https://img.example.com"
  tls:
    mode: autocert
    domain: "img.example.com"   # 必须与 public_base_url 的 host 一致
    email: "you@example.com"
    cache_dir: "./data/autocert"
    http_listen: ":80"
```

需要 80/443 可达、域名已解析到本机。证书缓存在 `data/autocert`，重启不会重复申请。

### 方式二：反向代理终止 TLS

```yaml
image_server:
  listen: "127.0.0.1:8088"
  public_base_url: "https://img.example.com"
  tls:
    mode: off
```

Caddyfile 示例：

```
img.example.com {
    reverse_proxy 127.0.0.1:8088
}
```

`public_base_url` 仍然是 HTTPS，校验只检查它是不是绝对 URL 以及协议组合是否合理。

## 常见故障

| 现象 | 原因 | 处理 |
|---|---|---|
| `/start` 无响应或日志报 401 | token 错误 | 检查 `telegram.token` |
| 回复「未授权：…（你的 ID：N）」 | 白名单未包含该用户 | 把 N 加进 `allowed_user_ids` 或临时 `allow_all_users: true` |
| 回复「需要有效的 E-Hentai Cookie」 | 未登录访问 `/fullimg` 或画廊被重定向到登录页 | 配置 `ehentai.cookie`（见 [docs/config.md](docs/config.md)） |
| 结果提示「已使用重采样图」 | 无 Cookie 或原图节点已失效 | 属预期降级；配置 Cookie 可获得原图 |
| 结果提示「站点拒绝访问」 | 触发风控/地区限制 | 调低 `bandwidth_kib_per_sec`、增大 `request_interval`，或配置代理 |
| 结果提示「被站点限流」 | 429 且重试用尽 | 稍后重试 |
| telegra.ph 页面图片打不开 | `public_base_url` 不是公网可达，或浏览器拒绝 http 图片 | 用浏览器直接访问 `https://<public_base_url>/healthz` 排查 |
| 发布报 `CONTENT_TOO_BIG` 且预算已降到 4096 | Telegraph 结构性限制变化 | 调小 `telegraph.max_content_bytes` 并检查内容构建逻辑 |
| exhentai 链接失败 | 表里站需要有效 Cookie（igneous） | 配置 exhentai 的 Cookie |
| 日志出现「图床公网地址不可达」 | 自检请求失败（DNS/反代/防火墙） | 修好公网访问；该告警不阻塞启动 |
| `public_base_url: https://localhost` + `tls.mode: off` | 本进程只在 `listen`（默认 `:8088`）上提供明文 HTTP，443 上没有服务 | 要么用 autocert，要么让反向代理把 `https://localhost` 转到 `127.0.0.1:8088`；否则 Telegraph 页面图片打不开 |
| 失败提示「画廊不存在、已删除或 token 无效」 | 站点返回 `Key missing, or incorrect key provided.` 或 `This gallery is not available.` | 重新复制完整画廊链接（含 token）再试 |
| 失败提示「需要有效的 E-Hentai Cookie」 | 站点返回 `You must be logged in`、Sad Panda 页，或 exhentai 返回 200 空响应 | 配置/更新 `ehentai.cookie`；exhentai 的 Cookie 必须包含 `igneous` |
| 失败提示「站点拒绝访问」并提到 Offensive | 画廊被标记为 Offensive，站点返回警告页 | 程序已自动附加 `nw=1` 跳过警告；仍失败说明当前 Cookie 不被接受 |
| 失败提示「页面解析失败…（文本片段：…）」 | 站点返回了未知结构或未知错误页 | 按括号内的站点原文判断；若为站点改版请更新解析器与 fixture |

## 已知限制

- 一条消息只处理一个链接；`/s/` 单页链接会被解析成整本画廊。
- 不做 `/archiver.php` 打包下载（需要 GP 点数，不可靠）。
- 不自动清理磁盘：Telegraph 页面长期有效，删除本地图片会让页面图片失效。
- 下载失败的页会被跳过并汇报数量；重新发送同一链接会增量补全。
- 外部依赖：telegra.ph 对 `img` 外链的策略可能变化（目前接受任意 URL）。

## 合规提示

请只在你有权访问、存储与分享的内容上使用本工具，并遵守 E-Hentai/ExHentai 的服务条款与当地法律。自建图床会把图片公网暴露，请自行评估版权与内容风险；`robots.txt` 默认 `Disallow: /` 只是阻止搜索引擎抓取，不构成任何保护。

## 文档

- [docs/architecture.md](docs/architecture.md)：组件、时序、状态机、并发与磁盘布局
- [docs/modules.md](docs/modules.md)：各包职责与接口
- [docs/decisions.md](docs/decisions.md)：架构决策记录（ADR）
- [docs/config.md](docs/config.md)：全部配置字段与 Cookie 获取方法
- [docs/development.md](docs/development.md)：目录结构、Make 目标、fixture 更新、调试方法
