package ehentai

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// GalleryPage 是一页缩略图页的解析结果。
type GalleryPage struct {
	// Start、End 是该页第一条/最后一条图片的序号（从 1 开始），Total 是整本画廊的图片总数。
	Start, End, Total int
	Pages             []ImagePage
}

// ImagePageContent 是单页图片页（/s/...）解析出的候选图片地址。
type ImagePageContent struct {
	// OriginalURL 指向 /fullimg/... 的原图；未登录时仍然存在，但请求会被重定向到登录页。
	OriginalURL string
	// ImageURL 是 img#img 的 src，通常是 H@H 的重采样图。
	ImageURL string
}

var (
	showingRe = regexp.MustCompile(`Showing\s+([\d,]+)\s*-\s*([\d,]+)\s+of\s+([\d,]+)\s+images`)
	pageRefRe = regexp.MustCompile(`/s/[0-9a-z]+/(\d+)-(\d+)`)
)

// parseGalleryPage 解析缩略图页：从 div#gdt 收集 /s/ 链接，从「Showing X - Y of N images」取边界。
// 站点改版时会返回 ErrParse 而不是静默产出空列表，避免"下载到 0 张"被误当成成功。
func parseGalleryPage(r io.Reader) (GalleryPage, error) {
	z := html.NewTokenizer(r)
	var (
		page     GalleryPage
		text     strings.Builder
		inGdt    bool
		gdtDepth int
		foundGdt bool
	)
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if err := z.Err(); err != nil && !errors.Is(err, io.EOF) {
				return GalleryPage{}, fmt.Errorf("%w: %v", ErrParse, err)
			}
			break
		}
		switch tt {
		case html.TextToken:
			text.WriteString(strings.ReplaceAll(string(z.Text()), "\u00a0", " "))
			text.WriteByte(' ')
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := strings.ToLower(string(name))
			var attrs map[string]string
			if hasAttr {
				attrs = readTagAttrs(z)
			}
			if tag == "div" {
				if !inGdt {
					if attrs["id"] == "gdt" {
						inGdt, gdtDepth, foundGdt = true, 1, true
						continue
					}
				} else if tt == html.StartTagToken {
					gdtDepth++
				}
			}
			if inGdt && tag == "a" {
				href := strings.TrimSpace(attrs["href"])
				if m := pageRefRe.FindStringSubmatch(href); m != nil {
					if pageNo, err := strconv.Atoi(m[2]); err == nil && pageNo > 0 && href != "" {
						page.Pages = append(page.Pages, ImagePage{Page: pageNo, URL: href})
					}
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if inGdt && strings.EqualFold(string(name), "div") {
				gdtDepth--
				if gdtDepth <= 0 {
					inGdt = false
				}
			}
		}
	}

	if !foundGdt {
		if err := classifyPageText(text.String()); err != nil {
			return GalleryPage{}, err
		}
		return GalleryPage{}, fmt.Errorf("%w: 未找到缩略图容器 div#gdt（文本片段：%s）", ErrParse, textSnippet(text.String(), 120))
	}
	m := showingRe.FindStringSubmatch(text.String())
	if m == nil {
		if err := classifyPageText(text.String()); err != nil {
			return GalleryPage{}, err
		}
		return GalleryPage{}, fmt.Errorf("%w: 未找到「Showing X - Y of N images」文本（文本片段：%s）", ErrParse, textSnippet(text.String(), 120))
	}
	var err error
	if page.Start, err = atoiComma(m[1]); err != nil {
		return GalleryPage{}, fmt.Errorf("%w: 起始序号 %q: %v", ErrParse, m[1], err)
	}
	if page.End, err = atoiComma(m[2]); err != nil {
		return GalleryPage{}, fmt.Errorf("%w: 结束序号 %q: %v", ErrParse, m[2], err)
	}
	if page.Total, err = atoiComma(m[3]); err != nil {
		return GalleryPage{}, fmt.Errorf("%w: 总数 %q: %v", ErrParse, m[3], err)
	}

	// gdt 内的链接可能是相对路径，调用方会按当前页面 URL 补全。
	sort.SliceStable(page.Pages, func(i, j int) bool { return page.Pages[i].Page < page.Pages[j].Page })
	return page, nil
}

// parseGalleryTitle 读取 <title> 并去掉站点后缀，供 gdata API 不可用时降级使用。
func parseGalleryTitle(r io.Reader) (string, error) {
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return "", fmt.Errorf("%w: 未找到 <title>", ErrParse)
		}
		if tt != html.StartTagToken {
			continue
		}
		name, _ := z.TagName()
		if !strings.EqualFold(string(name), "title") {
			continue
		}
		var b strings.Builder
		for {
			tt := z.Next()
			if tt == html.ErrorToken || tt == html.EndTagToken {
				break
			}
			if tt == html.TextToken {
				b.Write(z.Text())
			}
		}
		title := strings.TrimSpace(b.String())
		title = strings.TrimSuffix(title, " - E-Hentai Galleries")
		title = strings.TrimSuffix(title, " - ExHentai.org Galleries")
		return title, nil
	}
}

// parseImagePage 解析单页图片页：/fullimg/ 链接为原图，img#img 为重采样图。
func parseImagePage(r io.Reader) (ImagePageContent, error) {
	z := html.NewTokenizer(r)
	var c ImagePageContent
	var text strings.Builder
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if err := z.Err(); err != nil && !errors.Is(err, io.EOF) {
				return c, fmt.Errorf("%w: %v", ErrParse, err)
			}
			break
		}
		if tt == html.TextToken {
			text.WriteString(strings.ReplaceAll(string(z.Text()), "\u00a0", " "))
			text.WriteByte(' ')
			continue
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		if !hasAttr {
			continue
		}
		attrs := readTagAttrs(z)
		switch strings.ToLower(string(name)) {
		case "a":
			if href := attrs["href"]; href != "" && c.OriginalURL == "" && strings.Contains(href, "/fullimg/") {
				c.OriginalURL = href
			}
		case "img":
			if attrs["id"] == "img" && attrs["src"] != "" {
				c.ImageURL = attrs["src"]
			}
		}
	}
	if c.OriginalURL == "" && c.ImageURL == "" {
		if err := classifyPageText(text.String()); err != nil {
			return c, err
		}
		return c, fmt.Errorf("%w: 未找到 /fullimg/ 链接或 img#img（文本片段：%s）", ErrParse, textSnippet(text.String(), 120))
	}
	return c, nil
}

// readTagAttrs 读取当前标签的全部属性；调用前必须已调用 TagName。
func readTagAttrs(z *html.Tokenizer) map[string]string {
	attrs := make(map[string]string, 4)
	for {
		k, v, more := z.TagAttr()
		if len(k) > 0 {
			attrs[strings.ToLower(string(k))] = string(v)
		}
		if !more {
			return attrs
		}
	}
}

func atoiComma(s string) (int, error) {
	return strconv.Atoi(strings.ReplaceAll(s, ",", ""))
}

// pageErrorMarkers 把 E-Hentai 以 200 状态返回的纯文本错误页映射为哨兵错误，
// 否则这类页面只会表现为「解析失败」，无法判断是 token 失效、需要登录还是被限流。
var pageErrorMarkers = []struct {
	marker string
	err    error
}{
	{"key missing, or incorrect key provided", ErrNotFound},
	{"this gallery is not available", ErrNotFound},
	{"this gallery has been removed", ErrNotFound},
	{"this gallery is private", ErrNotFound},
	{"this gallery is inaccessible", ErrNotFound},
	{"you must be logged in", ErrAuthRequired},
	{"sad panda", ErrAuthRequired},
	{"temporarily banned", ErrForbidden},
	{"ip address has been banned", ErrForbidden},
	{"too many requests", ErrRateLimited},
	{"flagged as offensive", ErrForbidden},
	{"never warn me again", ErrForbidden},
}

// classifyPageText 在页面文本中匹配已知站点错误文案；未命中返回 nil。
func classifyPageText(text string) error {
	lower := strings.ToLower(text)
	for _, m := range pageErrorMarkers {
		if strings.Contains(lower, m.marker) {
			return fmt.Errorf("%w: 站点返回「%s」", m.err, m.marker)
		}
	}
	return nil
}

// textSnippet 把页面文本压成一行并截断，便于日志定位问题。
func textSnippet(text string, limit int) string {
	flat := strings.Join(strings.Fields(text), " ")
	r := []rune(flat)
	if len(r) > limit {
		return string(r[:limit]) + "..."
	}
	if len(r) == 0 {
		return "<空>"
	}
	return flat
}
