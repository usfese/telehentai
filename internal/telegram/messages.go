package telegram

import (
	"errors"
	"fmt"
	"strings"

	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/pipeline"
)

// helpText 是 /start 与 /help 的固定回复；面向普通用户，不暴露部署细节。
const helpText = `发送 e-hentai / exhentai 画廊链接，我会下载整本并生成一篇可直接阅读的网页图集。
支持：
  https://e-hentai.org/g/<gid>/<token>/
  https://exhentai.org/g/<gid>/<token>/
  https://e-hentai.org/s/<pagekey>/<gid>-<n>
发送 /start 可再次查看本说明。`

// linkHint 是收到非画廊文本时的提示。
const linkHint = "请发送 e-hentai / exhentai 画廊链接，例如 https://e-hentai.org/g/123456/abcdef1234/"

// busyText 在队列满或同一画廊正在处理时回复。
const busyText = "当前任务较多，请稍后再试。"

// noAccessText 在用户不在白名单时回复。
const noAccessText = "抱歉，你暂时没有使用权限。"

// progressText 把流水线进度翻译成简洁的中文状态。
func progressText(p pipeline.Progress) string {
	switch p.Stage {
	case pipeline.StageMeta:
		return "正在解析画廊…"
	case pipeline.StageListing:
		return "正在获取图片列表…"
	case pipeline.StageDownloading:
		if p.Total > 0 {
			return fmt.Sprintf("正在下载图片 %d/%d", p.Done, p.Total)
		}
		return "正在下载图片…"
	case pipeline.StagePublishing:
		return "正在生成网页…"
	case pipeline.StageDone:
		return "完成"
	default:
		return "处理中…"
	}
}

// resultText 生成最终成功消息；只保留用户关心的结果。
func resultText(res pipeline.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "已完成：%d 张图片，%d 个页面", res.Images, len(res.URLs))
	for _, u := range res.URLs {
		b.WriteString("\n")
		b.WriteString(u)
	}
	if res.Failed > 0 {
		b.WriteString("\n（部分图片未能获取）")
	}
	return b.String()
}

// errorText 把内部错误映射为用户可读的提示，不暴露 Cookie、配置项等部署细节。
func errorText(err error) string {
	switch {
	case err == nil:
		return "处理失败，请稍后再试。"
	case errors.Is(err, ehentai.ErrAuthRequired), errors.Is(err, ehentai.ErrForbidden):
		return "这本画廊暂时无法访问，请稍后再试或换一个链接。"
	case errors.Is(err, ehentai.ErrNotFound):
		return "没有找到这本画廊，链接可能已失效。"
	case errors.Is(err, ehentai.ErrRateLimited):
		return "站点繁忙，请稍后再试。"
	case errors.Is(err, ehentai.ErrParse):
		return "内容解析失败，请稍后再试。"
	default:
		return "处理失败，请稍后再试。"
	}
}
