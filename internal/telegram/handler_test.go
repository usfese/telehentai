package telegram

import "testing"

func TestIsHelpCommand(t *testing.T) {
	cases := map[string]bool{
		"/start":                               true,
		"/help":                                true,
		"/start@flanchan_bot":                  true,
		"/help@other_bot":                      true,
		"/gallery":                             false,
		"/":                                    false,
		"start":                                false,
		"https://e-hentai.org/g/1/abcdef1234/": false,
	}
	for in, want := range cases {
		if got := isHelpCommand(in); got != want {
			t.Fatalf("isHelpCommand(%q)=%v，期望 %v", in, got, want)
		}
	}
}

func TestExtractLink(t *testing.T) {
	if _, ok := extractLink("https://e-hentai.org/g/4310985/ad9552b3e4/"); !ok {
		t.Fatal("纯链接应被识别")
	}
	if _, ok := extractLink("求这本 https://e-hentai.org/g/4310985/ad9552b3e4/ 谢谢"); !ok {
		t.Fatal("带说明文字的链接应被识别")
	}
	if _, ok := extractLink("https://example.com/g/1/abcdef1234/"); ok {
		t.Fatal("非 EH 链接不应识别")
	}
	if _, ok := extractLink("普通聊天"); ok {
		t.Fatal("普通文本不应识别")
	}
}
