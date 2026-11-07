package ehentai

import (
	"errors"
	"testing"
)

func TestParseLink(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Link
		bad  bool
	}{
		{
			name: "画廊链接",
			raw:  "https://e-hentai.org/g/4310985/ad9552b3e4/",
			want: Link{Site: SiteEHentai, GID: 4310985, Token: "ad9552b3e4"},
		},
		{
			name: "画廊链接无斜杠结尾",
			raw:  "https://e-hentai.org/g/4310985/ad9552b3e4",
			want: Link{Site: SiteEHentai, GID: 4310985, Token: "ad9552b3e4"},
		},
		{
			name: "www 与 http 与查询参数",
			raw:  "http://www.e-hentai.org/g/123/abcdef1234/?p=3",
			want: Link{Site: SiteEHentai, GID: 123, Token: "abcdef1234"},
		},
		{
			name: "单页链接",
			raw:  "https://e-hentai.org/s/b29c977f6a/4310986-1",
			want: Link{Site: SiteEHentai, GID: 4310986, PageToken: "b29c977f6a", Page: 1},
		},
		{
			name: "exhentai 画廊",
			raw:  "https://exhentai.org/g/4310985/ad9552b3e4/",
			want: Link{Site: SiteExHentai, GID: 4310985, Token: "ad9552b3e4"},
		},
		{
			name: "exhentai 单页",
			raw:  "https://exhentai.org/s/abcdef1234/777-42/",
			want: Link{Site: SiteExHentai, GID: 777, PageToken: "abcdef1234", Page: 42},
		},
		{name: "其他站点", raw: "https://example.com/g/1/abcdef1234/", bad: true},
		{name: "非 http scheme", raw: "ftp://e-hentai.org/g/1/abcdef1234/", bad: true},
		{name: "token 太短", raw: "https://e-hentai.org/g/1/abc/", bad: true},
		{name: "token 含大写", raw: "https://e-hentai.org/g/1/ABCDEF1234/", bad: true},
		{name: "gid 非数字", raw: "https://e-hentai.org/g/abc/abcdef1234/", bad: true},
		{name: "不是画廊路径", raw: "https://e-hentai.org/", bad: true},
		{name: "空字符串", raw: "  ", bad: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseLink(tc.raw)
			if tc.bad {
				if err == nil {
					t.Fatalf("期望错误，得到 %+v", got)
				}
				if !errors.Is(err, ErrNotGalleryLink) {
					t.Fatalf("错误应可用 errors.Is(ErrNotGalleryLink) 判断，得到 %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLink: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseLink(%q)=%+v，期望 %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSiteHost(t *testing.T) {
	if got := SiteEHentai.BaseURL(); got != "https://e-hentai.org" {
		t.Fatalf("e-hentai BaseURL=%q", got)
	}
	if got := SiteExHentai.BaseURL(); got != "https://exhentai.org" {
		t.Fatalf("exhentai BaseURL=%q", got)
	}
}
