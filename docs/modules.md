# 模块说明

## 包一览

| 包 | 职责 | 依赖 |
|---|---|---|
| `cmd/telehentai` | 进程入口：信号处理 + `cli.Execute` | `internal/cli` |
| `internal/cli` | 子命令解析与命令实现（`run`/`fetch`/`publish`/`serve-images`/`version`） | 其余全部（装配层） |
| `internal/config` | YAML 加载、环境变量覆盖、按模式校验 | 无 |
| `internal/logx` | slog logger 构造 | 无 |
| `internal/ehentai` | 链接解析、限速、元数据、图片列表、图片下载与原图回退 | 无（仅标准库、`x/net/html`、`x/time/rate`） |
| `internal/store` | 本地画廊缓存（原子写）、Telegraph token 持久化 | `internal/ehentai`（仅类型） |
| `internal/telegraph` | telegra.ph 客户端与内容节点构建 | 无 |
| `internal/pipeline` | 任务队列、下载复用、分页发布 | `internal/ehentai`、`internal/telegraph` |
| `internal/imgsrv` | 只读图片 HTTP 服务、autocert | `internal/store` |
| `internal/telegram` | Telegram 长轮询、授权、进度与回执 | `internal/ehentai`、`internal/pipeline` |
| `internal/app` | 生产装配与生命周期 | 以上全部 |

依赖方向严格单向，`pipeline` 通过接口隔离网络与磁盘，测试不需要真实 E-Hentai。

## internal/config

```go
type Duration time.Duration
func (d Duration) D() time.Duration

type Config struct { Telegram; EHentai; Download; Telegraph; ImageServer; Pipeline; Log }
func Default() *Config
func Load(path string, explicit bool) (*Config, error)
func (c *Config) Validate(mode Mode) error
```

`Load` 先取内置默认值，再用文件覆盖，最后用环境变量补齐空字段。`explicit=false` 且文件缺失时直接返回默认值（便于无配置调试）。

## internal/ehentai

```go
func ParseLink(raw string) (Link, error)          // 链接解析，不联网
func (c *Client) ResolveRef(ctx, Link) (GalleryRef, error)  // /s/ 链接换 token
func (c *Client) Meta(ctx, GalleryRef) (GalleryMeta, error) // gdata，失败回退 HTML
func (c *Client) ListImagePages(ctx, GalleryRef, expected int) ([]ImagePage, error)
func (c *Client) ResolveImage(ctx, ImagePage) (ImageRef, error)
func (c *Client) Open(ctx, ImageRef) (io.ReadCloser, ImageData, error)
func New(Config, *slog.Logger) (*Client, error)
```

关键类型：`GalleryMeta`（元数据）、`ImagePage`（页码 + `/s/` 链接）、`ImageRef`（原图/重采样图候选）、`ImageData`（Content-Type、扩展名、大小、`UsedFallback`）。

## internal/store

`Store` 只依赖文件系统：`New(root)` 建目录；`SaveMeta`/`LoadMeta` 读写 `meta.json`（临时文件 + rename）；`SaveImage`/`ImageExists`/`ListImages`/`HasAll` 管理 `<root>/galleries/<gid>/<页码>.<ext>`；`TelegraphToken`/`SaveTelegraphToken` 管理 `<root>/telegraph.json`（0600，缺失时返回空串而非错误）。

## internal/telegraph

EnsureToken 按「配置 token、持久化 token、自动创建账号」的顺序取 token，创建后写入 store。CreatePage/EditPage 以表单提交 content，并把 CONTENT_TOO_BIG 映射为 ErrContentTooBig，token 失效与 FLOOD_WAIT 各重试一次。content.BuildPart 按节点 JSON 字节数切分图片，保留 512 字节给导航节点；标题按 rune 截断到 256。

## internal/pipeline

- 任务队列：New/Start/Submit/Process；Start 启动 workers 个 goroutine，Submit 用 inflight map 去重并做非阻塞入队。
- Source / Publisher / ImageStore 三个接口是测试边界；生产实现分别是 ehentai.Client、telegraph.Client、store.Store。
- DownloadGallery 是 fetch 子命令与 Process 共用的下载循环：ListImagePages → 逐页 ImageExists → ResolveImage → Open → SaveImage，单页失败重解析重试一次，结束写 meta。
- PublishImages 是 publish 子命令与 Process 共用的发布循环：BuildPart 切分、createPage、给上一页 editPage 补「下一部分」；ErrContentTooBig 时预算乘 0.6 重试。

## internal/imgsrv

New 构造 Server，Handler 返回 ServeMux（路由 GET /i/{gid}/{name}、/healthz、/robots.txt），Run 负责监听与优雅关闭。文件名白名单正则与 gid 校验共同杜绝路径穿越；响应带一年期 immutable 缓存头。tls.mode=autocert 时固定监听 :443，并另起 HTTP 监听做 ACME 挑战与 302 跳转。

## internal/telegram

Bot 封装 go-telegram/bot 的长轮询。handle 负责白名单校验、/start 与 /help、ParseLink、ResolveRef、发送状态消息、Submit，以及进度与最终结果的编辑（含 message is not modified 忽略）。Resolver 接口用于补全单页链接的 token。

## internal/app

Run 完成装配：store → ehentai → telegraph → pipeline → imgsrv → telegram；启动流水线、图床、机器人，任一组件失败即取消整体并返回首个错误。同时导出 EhentaiConfig、PipelineConfig、TelegraphConfig、ImageServerConfig 四个映射函数，供 CLI 复用，避免配置映射重复。

## 扩展点

- **新增站点**：在 ehentai 包内实现与 Client 相同的方法集，再给 pipeline 传入新的 Source 实现。
- **替换图床**：imgsrv 只需实现 GET /i/{gid}/{name} 与 /healthz；只要公开 URL 结构不变，pipeline 与 telegraph 都无需改动。
- **替换发布端**：实现 pipeline.Publisher（CreatePage/EditPage）即可换成别的博客平台。
- **替换存储**：实现 pipeline.ImageStore 与 telegraph.TokenStore，可把本地磁盘换成对象存储。





