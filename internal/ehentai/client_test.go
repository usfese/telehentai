package ehentai

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCookieHeader(t *testing.T) {
	cases := []struct {
		name   string
		cookie string
		want   string
	}{
		{"无 Cookie 也附加 nw", "", "nw=1"},
		{"追加 nw", "ipb_member_id=1; ipb_pass_hash=x", "ipb_member_id=1; ipb_pass_hash=x; nw=1"},
		{"已含 nw 不重复", "nw=1; ipb_member_id=1", "nw=1; ipb_member_id=1"},
		{"末尾分号不产生空项", "a=1;", "a=1; nw=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &cookieTransport{cookie: tc.cookie}
			if got := tr.cookieHeader(); got != tc.want {
				t.Fatalf("cookieHeader()=%q，期望 %q", got, tc.want)
			}
		})
	}
}

func TestCookieOnlySentToEHosts(t *testing.T) {
	for _, host := range []string{"e-hentai.org", "www.e-hentai.org", "api.e-hentai.org", "exhentai.org"} {
		if !isECookieHost(host) {
			t.Fatalf("%s 应允许附加 Cookie", host)
		}
	}
	for _, host := range []string{"hath.network", "ehgt.org", "example.com", "not-e-hentai.org.evil.com"} {
		if isECookieHost(host) {
			t.Fatalf("%s 不应收到 Cookie", host)
		}
	}
}

func TestRedirectToSadPandaIsAuthRequired(t *testing.T) {
	c, err := New(Config{RequestInterval: time.Millisecond}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, "https://exhentai.org/?poni=no", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.http.CheckRedirect == nil {
		t.Fatal("未配置 CheckRedirect")
	}
	if err := c.http.CheckRedirect(req, nil); !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("期望 ErrAuthRequired，得到 %v", err)
	}
}

func TestOffensiveWarningPageClassified(t *testing.T) {
	body := `<html><body><p>This gallery has been flagged as Offensive For Everyone.
	Due to its content, it should not be viewed by anyone.</p>
	<a href="?nw=never">Never Warn Me Again</a></body></html>`
	_, err := parseGalleryPage(strings.NewReader(body))
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("期望 ErrForbidden，得到 %v", err)
	}
}

func TestEmptyResponseError(t *testing.T) {
	if err := emptyResponseError("https://exhentai.org/g/1/tok/?p=0"); !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("exhentai 空响应应为 ErrAuthRequired，得到 %v", err)
	}
	if err := emptyResponseError("https://e-hentai.org/g/1/tok/?p=0"); !errors.Is(err, ErrParse) {
		t.Fatalf("e-hentai 空响应应为 ErrParse，得到 %v", err)
	}
}

func TestResolveURL(t *testing.T) {
	if got := resolveURL("https://e-hentai.org/s/key/1-1", "/fullimg/1/1/k/1.png"); got != "https://e-hentai.org/fullimg/1/1/k/1.png" {
		t.Fatalf("相对路径解析错误: %s", got)
	}
	if got := resolveURL("https://e-hentai.org/s/key/1-1", "https://hath.network/h/x"); got != "https://hath.network/h/x" {
		t.Fatalf("绝对路径被改写: %s", got)
	}
}
