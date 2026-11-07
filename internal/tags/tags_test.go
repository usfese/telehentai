package tags

import "testing"

func TestTranslate(t *testing.T) {
	cases := []struct {
		tag  string
		want string
	}{
		{"female:lolicon", "女性：萝莉"},
		{"language:chinese", "语言：汉语"},
		{"unknownnamespace:whatever", "unknownnamespace:whatever"},
		{"female:not a real tag", "female:not a real tag"},
		{"notatag", "notatag"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := Translate(tc.tag); got != tc.want {
			t.Fatalf("Translate(%q)=%q，期望 %q", tc.tag, got, tc.want)
		}
	}
}

func TestVersionLoaded(t *testing.T) {
	if Version() == "" {
		t.Fatal("内嵌数据库未加载版本号")
	}
}
