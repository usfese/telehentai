package ehentai

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("打开 fixture %s: %v", name, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestParseGalleryPageFirstPage(t *testing.T) {
	gp, err := parseGalleryPage(openFixture(t, "gallery_22.html"))
	if err != nil {
		t.Fatalf("parseGalleryPage: %v", err)
	}
	if gp.Start != 1 || gp.End != 20 || gp.Total != 22 {
		t.Fatalf("边界错误: start=%d end=%d total=%d", gp.Start, gp.End, gp.Total)
	}
	if len(gp.Pages) != 20 {
		t.Fatalf("期望 20 个链接，得到 %d", len(gp.Pages))
	}
	for i, p := range gp.Pages {
		if p.Page != i+1 {
			t.Fatalf("第 %d 个链接页码为 %d，期望升序从 1 开始", i, p.Page)
		}
		if !strings.Contains(p.URL, "/s/") {
			t.Fatalf("链接不是图片页: %q", p.URL)
		}
	}
}

func TestParseGalleryPageLastPage(t *testing.T) {
	gp, err := parseGalleryPage(openFixture(t, "gallery_22_p2.html"))
	if err != nil {
		t.Fatalf("parseGalleryPage: %v", err)
	}
	if gp.Start != 21 || gp.End != 22 || gp.Total != 22 {
		t.Fatalf("边界错误: start=%d end=%d total=%d", gp.Start, gp.End, gp.Total)
	}
	if len(gp.Pages) != 2 || gp.Pages[0].Page != 21 || gp.Pages[1].Page != 22 {
		t.Fatalf("末页链接错误: %+v", gp.Pages)
	}
}

func TestParseGalleryPageThousandsSeparator(t *testing.T) {
	gp, err := parseGalleryPage(openFixture(t, "gallery_1400.html"))
	if err != nil {
		t.Fatalf("parseGalleryPage: %v", err)
	}
	if gp.Total != 1400 {
		t.Fatalf("千分位数字解析错误，total=%d", gp.Total)
	}
	if gp.Start != 1 || gp.End != 20 || len(gp.Pages) != 20 {
		t.Fatalf("首页边界错误: start=%d end=%d n=%d", gp.Start, gp.End, len(gp.Pages))
	}
}

func TestParseGalleryPageNotGallery(t *testing.T) {
	_, err := parseGalleryPage(strings.NewReader("<html><body>nothing here</body></html>"))
	if err == nil || !errors.Is(err, ErrParse) {
		t.Fatalf("期望 ErrParse，得到 %v", err)
	}
	if !strings.Contains(err.Error(), "nothing here") {
		t.Fatalf("错误应包含页面文本片段便于定位，得到 %v", err)
	}
}

// 站点对失效 token、登录要求、限流等情况会返回 200 的纯文本/HTML 页面，
// 必须识别成具体错误，而不是笼统的解析失败。
func TestParseGalleryPageClassifiesSiteErrorPages(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{"token 失效", "<html><body>Key missing, or incorrect key provided.</body></html>", ErrNotFound},
		{"画廊不可用", "<html><body>This gallery is not available.</body></html>", ErrNotFound},
		{"需要登录", "<html><body>You must be logged in to view this gallery.</body></html>", ErrAuthRequired},
		{"IP 封禁", "<html><body>Your IP address has been temporarily banned.</body></html>", ErrForbidden},
		{"限流", "<html><body>Too many requests.</body></html>", ErrRateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseGalleryPage(strings.NewReader(tc.body))
			if !errors.Is(err, tc.want) {
				t.Fatalf("期望 %v，得到 %v", tc.want, err)
			}
		})
	}
}

func TestParseGalleryTitle(t *testing.T) {
	title, err := parseGalleryTitle(openFixture(t, "gallery_22.html"))
	if err != nil {
		t.Fatalf("parseGalleryTitle: %v", err)
	}
	if strings.HasSuffix(title, "Galleries") || title == "" {
		t.Fatalf("标题后缀未去除: %q", title)
	}
	if !strings.Contains(title, "Synthetic Test Gallery") {
		t.Fatalf("标题内容异常: %q", title)
	}
}

func TestParseImagePage(t *testing.T) {
	c, err := parseImagePage(openFixture(t, "image_page.html"))
	if err != nil {
		t.Fatalf("parseImagePage: %v", err)
	}
	if !strings.Contains(c.OriginalURL, "/fullimg/4310986/1/") {
		t.Fatalf("原图地址错误: %q", c.OriginalURL)
	}
	if !strings.Contains(c.ImageURL, "hath.network") || !strings.HasSuffix(c.ImageURL, ".webp") {
		t.Fatalf("重采样图地址错误: %q", c.ImageURL)
	}
}

func TestParseImagePageNoImages(t *testing.T) {
	_, err := parseImagePage(strings.NewReader("<html><body><p>登录</p></body></html>"))
	if err == nil || !errors.Is(err, ErrParse) {
		t.Fatalf("期望 ErrParse，得到 %v", err)
	}
}

func TestParseImagePageAuthRequired(t *testing.T) {
	_, err := parseImagePage(strings.NewReader("<html><body>You must be logged in to view this image.</body></html>"))
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("期望 ErrAuthRequired，得到 %v", err)
	}
}
