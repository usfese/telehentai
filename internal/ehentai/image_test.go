package ehentai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestExtFromContentType(t *testing.T) {
	cases := map[string]string{
		"image/webp":                ".webp",
		"image/jpeg":                ".jpg",
		"image/jpeg; charset=utf-8": ".jpg",
		"image/png":                 ".png",
		"image/gif":                 ".gif",
	}
	for ct, want := range cases {
		got, err := extFromContentType(ct)
		if err != nil {
			t.Fatalf("%s: %v", ct, err)
		}
		if got != want {
			t.Fatalf("%s: 期望 %s，得到 %s", ct, want, got)
		}
	}

	for _, ct := range []string{"text/html", "application/octet-stream", ""} {
		_, err := extFromContentType(ct)
		if err == nil || !strings.Contains(err.Error(), "不支持的图片类型") {
			t.Fatalf("%q 应返回不支持错误，得到 %v", ct, err)
		}
	}
}

func TestImageReaderLimit(t *testing.T) {
	body := io.NopCloser(bytes.NewReader(make([]byte, 16)))
	r := &imageReader{body: body, ctx: context.Background(), limit: 8}
	_, err := io.ReadAll(r)
	if err == nil || !errors.Is(err, ErrParse) || !strings.Contains(err.Error(), "图片过大") {
		t.Fatalf("期望“图片过大”错误，得到 %v", err)
	}
}

func TestImageReaderNoLimitCounts(t *testing.T) {
	body := io.NopCloser(bytes.NewReader(make([]byte, 100)))
	r := &imageReader{body: body, ctx: context.Background()}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 100 || r.n != 100 {
		t.Fatalf("计数错误: len=%d n=%d", len(got), r.n)
	}
}

func TestNewBandwidthLimiter(t *testing.T) {
	if newBandwidthLimiter(0) != nil {
		t.Fatal("0 应表示不限速")
	}
	lim := newBandwidthLimiter(8)
	if lim == nil || lim.Burst() != 32*1024 {
		t.Fatalf("限速器构造错误: %+v", lim)
	}
}
