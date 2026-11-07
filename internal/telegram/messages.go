package telegram

import (
	"errors"
	"fmt"
	"strings"

	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/pipeline"
)

// helpText 是 /start 与 /help 的固定回复。
const helpText = `发送 e-hentai / exhentai 画廊链接，我会下载整本并生成 Telegraph 页面。
支持格式：
  https://e-hentai.org/g/<gid>/<token>/
  https://exhentai.org/g/<gid>/<token>/
  https://e-hentai.org/s/<pagekey>/<gid>-<n>
注意：原图需要配置 E-Hentai 登录 Cookie；未配置时使用重采样图。`

// linkHint 是收到非画廊文本时的提示。
const linkHint = "请发送 e-hentai / exhentai 画廊链接，例如 https://e-hentai.org/g/123456/abcdef1234/"

// progressText 把流水线进度翻译成中文状态文案。
func progressText(p pipeline.Progress) string {
	var text string
	switch p.Stage {
	case pipeline.StageMeta:
		text = "解析元数据…"
	case pipeline.StageListing:
		text = "获取图片列表…"
	case pipeline.StageDownloading:
		if p.Note == "缓存命中" {
			text = fmt.Sprintf("使用缓存（%d 张）", p.Total)
		} else {
			text = fmt.Sprintf("下载中 %d/%d", p.Done, p.Total)
		}
	case pipeline.StagePublishing:
		text = "生成 Telegraph 页面…"
	case pipeline.StageDone:
		text = "完成"
	default:
		text = "处理中…"
	}
	if p.Note != "" && p.Note != "缓存命中" {
		text += " · " + p.Note
	}
	return text
}

// resultText 生成最终成功消息。
func resultText(res pipeline.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "完成：%d 张图，%d 个页面", res.Images, len(res.URLs))
	for _, u := range res.URLs {
		b.WriteString("\n")
		b.WriteString(u)
	}
	if res.UsedFallback {
		b.WriteString("\n注意：未取得原图，已使用重采样图")
	}
	if res.Failed > 0 {
		fmt.Fprintf(&b, "\n其中 %d 页下载失败", res.Failed)
	}
	return b.String()
}

// errorText 把内部错误映射成用户可读的中文提示。
func errorText(err error) string {
	switch {
	case err == nil:
		return "处理失败：未知错误"
	case errors.Is(err, ehentai.ErrAuthRequired):
		return "需要有效的 E-Hentai Cookie（config.yaml → ehentai.cookie；访问 exhentai 还需要 igneous）"
	case errors.Is(err, ehentai.ErrNotFound):
		return "画廊不存在、已删除或 token 无效"
	case errors.Is(err, ehentai.ErrForbidden):
		return "站点拒绝访问：可能触发了风控，或该画廊被标记为 Offensive（需要有效 Cookie）"
	case errors.Is(err, ehentai.ErrRateLimited):
		return "被站点限流，请稍后重试"
	case errors.Is(err, ehentai.ErrParse):
		return "页面解析失败，站点结构可能已变化"
	default:
		return "处理失败：" + err.Error()
	}
}
