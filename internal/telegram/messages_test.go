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

func TestTagLinesUseHashtags(t *testing.T) {
	lines := tagLines([]string{"female:lolicon", "male:shotacon", "artist:gyuunyuu jirou"})
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"女性：#萝莉", "男性：#正太", "#gyuunyuu_jirou"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺少 %q: %s", want, joined)
		}
	}
}

func TestRejectedText(t *testing.T) {
	res := pipeline.Result{
		Rejected:  true,
		Total:     500,
		Limit:     200,
		Title:     "标题",
		SourceURL: "https://e-hentai.org/g/1/abcdef1234/",
		Tags:      []string{"female:lolicon"},
	}
	got := rejectedText(res)
	for _, want := range []string{"共 500 页，超过上限 200 页，未下载。", "女性：#萝莉", "https://e-hentai.org/g/1/abcdef1234/"} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺少 %q: %s", want, got)
		}
	}
}
