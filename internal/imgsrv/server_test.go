package imgsrv

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/usfese/telehentai/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	srv, err := New(Config{Listen: "127.0.0.1:0", PublicBaseURL: "http://127.0.0.1:1234"}, st, nil)
	if err != nil {
		t.Fatalf("imgsrv.New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func TestServeImage(t *testing.T) {
	ts, st := newTestServer(t)
	if _, err := st.SaveImage(4310985, 1, ".webp", strings.NewReader("webp-bytes")); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(ts.URL + "/i/4310985/0001.webp")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码=%d", resp.StatusCode)
	}
	if string(body) != "webp-bytes" {
		t.Fatalf("body=%q", body)
	}
	want := map[string]string{
		"Content-Type":           "image/webp",
		"Cache-Control":          "public, max-age=31536000, immutable",
		"X-Content-Type-Options": "nosniff",
		"Content-Disposition":    "inline",
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Fatalf("%s=%q，期望 %q", k, got, v)
		}
	}

	// HEAD 应与 GET 一样命中（ServeMux 的 GET 模式隐含支持 HEAD）。
	headResp, err := http.Head(ts.URL + "/i/4310985/0001.webp")
	if err != nil {
		t.Fatal(err)
	}
	headResp.Body.Close()
	if headResp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD 状态码=%d", headResp.StatusCode)
	}
}

func TestRejectsInvalidRequests(t *testing.T) {
	ts, st := newTestServer(t)
	if _, err := st.SaveImage(4310985, 1, ".webp", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		"/i/4310985/..%2f..%2fetc/passwd",
		"/i/4310985/0001.txt",
		"/i/4310985/0001.webp.bak",
		"/i/999/0001.webp",       // 不存在的画廊
		"/i/abc/0001.webp",       // 非法 gid
		"/i/0/0001.webp",         // gid 必须 > 0
		"/i/123456789/0001.webp", // gid 过长
		"/i/4310985/",            // 缺少文件名
	}
	for _, p := range paths {
		req, err := http.NewRequest(http.MethodGet, ts.URL+p, nil)
		if err != nil {
			t.Fatalf("构造请求 %s: %v", p, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("%s 应被拒绝，却返回 200", p)
		}
	}

	// 非 GET/HEAD 应 405。
	resp, err := http.Post(ts.URL+"/i/4310985/0001.webp", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST 状态码=%d，期望 405", resp.StatusCode)
	}
}

func TestRobotsAndHealth(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/robots.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Disallow: /") {
		t.Fatalf("robots.txt=%q", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q", ct)
	}

	hresp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	hresp.Body.Close()
	if hresp.StatusCode != http.StatusOK {
		t.Fatalf("healthz 状态码=%d", hresp.StatusCode)
	}
}
