package telegram

import (
	"errors"
	"fmt"
	"html"
	"strings"
	"unicode"

	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/pipeline"
	"github.com/usfese/telehentai/internal/tags"
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

// resultText 返回「画廊信息 + 页面链接」，链接显示为本子标题（需要 HTML parse mode）。
func resultText(res pipeline.Result) string {
	lines := infoLines(res)
	links := make([]string, 0, len(res.Pages)+len(res.URLs))
	if len(res.Pages) > 0 {
		for _, p := range res.Pages {
			links = append(links, fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(p.URL), html.EscapeString(p.Title)))
		}
	} else {
		links = append(links, res.URLs...)
	}
	for i := range lines {
		lines[i] = html.EscapeString(lines[i])
	}
	if len(links) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, links...)
	}
	return strings.Join(lines, "\n")
}

// rejectedText 是「画廊超过页数上限」时的说明。
func rejectedText(res pipeline.Result) string {
	lines := make([]string, 0, 8)
	if title := strings.TrimSpace(res.Title); title != "" {
		lines = append(lines, title)
	}
	lines = append(lines, fmt.Sprintf("共 %d 页，超过上限 %d 页，未下载。", res.Total, res.Limit))
	lines = append(lines, tagLines(res.Tags)...)
	if res.SourceURL != "" {
		lines = append(lines, "原画廊："+res.SourceURL)
	}
	for i := range lines {
		lines[i] = html.EscapeString(lines[i])
	}
	return strings.Join(lines, "\n")
}

// infoLines 返回标题、标签行与源链接（未转义）。
func infoLines(res pipeline.Result) []string {
	lines := make([]string, 0, 8)
	if title := strings.TrimSpace(res.Title); title != "" {
		lines = append(lines, title)
	}
	lines = append(lines, tagLines(res.Tags)...)
	if res.SourceURL != "" {
		lines = append(lines, "原画廊："+res.SourceURL)
	}
	return lines
}

// tagLines 按命名空间分组，每个标签加 #，在 Telegram 里可直接点击搜索。
func tagLines(list []string) []string {
	if len(list) > 50 {
		list = list[:50]
	}
	lines := make([]string, 0, 8)
	for _, g := range tags.GroupTags(list) {
		hashes := make([]string, 0, len(g.Tags))
		for _, name := range g.Tags {
			if h := hashtag(name); h != "" {
				hashes = append(hashes, "#"+h)
			}
		}
		if len(hashes) == 0 {
			continue
		}
		line := strings.Join(hashes, " ")
		if g.Namespace != "" && g.Namespace != "标签" {
			line = g.Namespace + "：" + line
		}
		lines = append(lines, line)
	}
	return lines
}

// hashtag 清洗标签名：保留字母、数字与下划线，空白转下划线，其余标点丢弃。
func hashtag(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
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
