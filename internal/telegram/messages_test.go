package telegram

import (
	"strings"
	"testing"

	"github.com/usfese/telehentai/internal/pipeline"
)

func TestResultTextUsesTitlesAndEscapes(t *testing.T) {
	res := pipeline.Result{
		Pages: []pipeline.PageLink{
			{URL: "https://telegra.ph/a-11-07", Title: "标题"},
			{URL: "https://telegra.ph/b-11-07", Title: "A&B <x>"},
		},
	}
	got := resultText(res)
	for _, want := range []string{
		`<a href="https://telegra.ph/a-11-07">标题</a>`,
		`A&amp;B &lt;x&gt;`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("结果缺少 %q: %s", want, got)
		}
	}
	if strings.Contains(got, "已完成") {
		t.Fatalf("不应包含统计行: %s", got)
	}
}

func TestResultTextFallsBackToURLs(t *testing.T) {
	if got := resultText(pipeline.Result{URLs: []string{"https://telegra.ph/x"}}); got != "https://telegra.ph/x" {
		t.Fatalf("got %q", got)
	}
}
