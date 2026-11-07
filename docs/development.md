# 开发指南

## 目录结构

```
cmd/telehentai/       进程入口（信号处理 + cli.Execute）
internal/app/         生产装配与生命周期
internal/cli/         子命令解析与实现
internal/config/      YAML 配置、环境变量、校验
internal/logx/        slog 构造
internal/ehentai/     链接/元数据/图片列表/下载/限速
internal/store/       本地缓存与 token 持久化
internal/telegraph/   telegra.ph 客户端与内容节点
internal/pipeline/    队列、下载复用、分页发布
internal/imgsrv/      只读图床
internal/telegram/    机器人
docs/                 架构、模块、决策、配置、开发文档
```

## 常用命令

```bash
make build   # 构建到 bin/telehentai
make test    # go test ./...
make vet     # go vet ./...
make fmt     # 检查 gofmt（有未格式化文件即失败）
make run     # go run ./cmd/telehentai run
make tidy    # go mod tidy
```

先跑 `make fmt vet test` 再提交；CI（`.github/workflows/ci.yml`）执行同样的检查。

## 测试约定

- 单测不得访问网络：HTML 解析使用 `internal/ehentai/testdata/` 中的**合成页面**，HTTP 客户端行为使用 `httptest`。
- 合成 fixture 覆盖：画廊首页、末页、含千分位总数的页面、单页图片页。它们只保留解析器依赖的结构（`div#gdt`、`/s/` 链接、`Showing X - Y of N images`、`img#img`、`/fullimg/`），不含任何站点真实内容。
- pipeline 使用 fake Source/Publisher/Store，验证下载跳过、单页失败容错、预算回退、并发与去重。

## 维护 fixture

fixture 是手写的最小 HTML，不是抓取快照。需要新增解析场景时，直接编辑 `internal/ehentai/testdata/*.html`：

- `gallery_22.html` / `gallery_22_p2.html`：22 页画廊的首页与末页，用于分页边界与链接顺序。
- `gallery_1400.html`：`Showing 1 - 20 of 1,400 images`，用于千分位解析。
- `image_page.html`：同时包含 `/fullimg/` 原图链接与 `img#img` 重采样图链接。

站点改版时，把新结构的最小片段补进对应文件，并同步 `parse_test.go` 的断言，**不要**把真实页面整页提交到仓库。

## 更新标签翻译数据

```bash
python3 scripts/update_tags.py          # 下载最新 release 并重写 internal/tags/data/tags.json
go test ./internal/tags/
```

脚本只保留「命名空间 → 标签 → 中文名」，不带简介，生成文件约 1.3 MB 并随二进制内嵌。数据许可与来源见 `internal/tags/data/README.md`。

## 只用 CLI 调试

不需要 Telegram token 也能验证下载、图床与发布：

```bash
telehentai fetch --limit 3 --data-dir /tmp/thdata "https://e-hentai.org/g/<gid>/<token>/"
telehentai serve-images --data-dir /tmp/thdata --listen 127.0.0.1:18088 &
curl -sI http://127.0.0.1:18088/i/<gid>/0001.jpg
telehentai publish --data-dir /tmp/thdata --image-base-url http://127.0.0.1:18088 <gid>
```

`publish` 会把 telegra.ph 页面写死到 `public_base_url` 指向的地址，本地冒烟可用 localhost（浏览器无法加载图片属预期）。

## 提交约定

- 一个提交只做一件事；消息使用 `feat(scope): 中文描述`、`fix(scope): ...`、`chore: ...`、`docs: ...`。
- 导出类型/函数必须有中文注释；限速、回退、预算这类「为什么」逻辑写 1~3 行说明，不做逐行翻译。
- 修改配置字段时同步更新 `config.example.yaml`、`docs/config.md` 与 `internal/config/config_test.go`（默认值一致性测试会强制这一点）。

## CI/CD

推送 `main` 自动跑测试、构建 linux/amd64 二进制并部署到 VPS（见 `.github/workflows/ci.yml`）。部署密钥与服务器信息存放在仓库 Secrets：`DEPLOY_HOST`、`DEPLOY_USER`、`DEPLOY_PORT`、`DEPLOY_SSH_KEY`。
