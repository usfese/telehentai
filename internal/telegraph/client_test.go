package telegraph

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTokens struct {
	tok   string
	saved int
}

func (f *fakeTokens) Load() (string, error) { return f.tok, nil }

func (f *fakeTokens) Save(s string) error {
	f.tok = s
	f.saved++
	return nil
}

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *fakeTokens) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	tokens := &fakeTokens{tok: "test-token"}
	c := New(Config{AuthorName: "telehentai", RequestInterval: 0}, tokens,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.apiBase = srv.URL
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c, tokens
}

func TestCreatePageOK(t *testing.T) {
	c, tokens := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/createPage" {
			t.Errorf("路径=%s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if got := r.PostForm.Get("access_token"); got != "test-token" {
			t.Errorf("token=%q", got)
		}
		if got := r.PostForm.Get("content"); !strings.Contains(got, `"tag":"p"`) {
			t.Errorf("content=%q", got)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"path":"test-09-06","title":"标题","url":"https://telegra.ph/test-09-06"}}`))
	})

	page, err := c.CreatePage(context.Background(), PageInput{
		Title:   "标题",
		Content: []Node{{Tag: "p", Children: []any{"hi"}}},
	})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.Path != "test-09-06" || page.URL != "https://telegra.ph/test-09-06" || page.Title != "标题" {
		t.Fatalf("返回页面错误: %+v", page)
	}
	if tokens.saved != 0 {
		t.Fatalf("已配置 token 时不应重新保存")
	}
}

func TestCreatePageContentTooBig(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"CONTENT_TOO_BIG"}`))
	})
	_, err := c.CreatePage(context.Background(), PageInput{Title: "t"})
	if !errors.Is(err, ErrContentTooBig) {
		t.Fatalf("期望 ErrContentTooBig，得到 %v", err)
	}
}

func TestCreatePageFloodWaitRetriesOnce(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"ok":false,"error":"FLOOD_WAIT_2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"path":"p","title":"t"}}`))
	})
	page, err := c.CreatePage(context.Background(), PageInput{Title: "t"})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if page.Path != "p" {
		t.Fatalf("page=%+v", page)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("应重试一次（共 2 次请求），实际 %d 次", got)
	}
}

func TestCreatePageAccessTokenInvalidRetriesOnce(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"ok":false,"error":"ACCESS_TOKEN_INVALID"}`))
	})
	_, err := c.CreatePage(context.Background(), PageInput{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "ACCESS_TOKEN_INVALID") {
		t.Fatalf("期望 token 失效错误，得到 %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("只应重试一次（共 2 次请求），实际 %d 次", got)
	}
}

func TestEnsureTokenCreatesAccount(t *testing.T) {
	var created atomic.Bool
	c, tokens := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/createAccount":
			created.Store(true)
			if sn := r.URL.Query().Get("short_name"); !strings.HasPrefix(sn, "telehentai-") {
				t.Errorf("short_name=%q", sn)
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"access_token":"auto-token"}}`))
		case "/createPage":
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			if got := r.PostForm.Get("access_token"); got != "auto-token" {
				t.Errorf("应使用自动创建的 token，得到 %q", got)
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"path":"p","title":"t"}}`))
		default:
			t.Errorf("未预期路径 %s", r.URL.Path)
		}
	})
	tokens.tok = "" // 既无配置 token 也无持久化 token
	c.cfgToken = ""

	if _, err := c.CreatePage(context.Background(), PageInput{Title: "t"}); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if !created.Load() {
		t.Fatal("未调用 createAccount")
	}
	if tokens.saved != 1 || tokens.tok != "auto-token" {
		t.Fatalf("自动 token 未持久化: saved=%d tok=%q", tokens.saved, tokens.tok)
	}
}

func TestEditPageSendsPath(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editPage" {
			t.Errorf("路径=%s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if got := r.PostForm.Get("path"); got != "some-path" {
			t.Errorf("path=%q", got)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"path":"some-path","title":"t"}}`))
	})
	page, err := c.EditPage(context.Background(), "some-path", PageInput{Title: "t"})
	if err != nil {
		t.Fatalf("EditPage: %v", err)
	}
	if page.Path != "some-path" {
		t.Fatalf("page=%+v", page)
	}
}

func TestEditPageRequiresPath(t *testing.T) {
	c := New(Config{}, nil, nil)
	if _, err := c.EditPage(context.Background(), "", PageInput{Title: "t"}); err == nil {
		t.Fatal("空 path 应报错")
	}
}
