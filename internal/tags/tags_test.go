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

func TestGroupTags(t *testing.T) {
	groups := GroupTags([]string{"language:chinese", "male:shotacon", "artist:nobody", "female:lolicon", "other:3d", "female:sole female"})
	wantLabels := []string{"女性", "男性", "语言", "艺术家", "其他"}
	if len(groups) != len(wantLabels) {
		t.Fatalf("分组数=%d: %+v", len(groups), groups)
	}
	for i, want := range wantLabels {
		if groups[i].Namespace != want {
			t.Fatalf("第 %d 组命名空间=%q，期望 %q", i, groups[i].Namespace, want)
		}
	}
	if got := groups[0].Tags; len(got) != 2 || got[0] != "萝莉" || got[1] != "单女主" {
		t.Fatalf("女性组=%v", got)
	}
	if got := groups[1].Tags; len(got) != 1 || got[0] != "正太" {
		t.Fatalf("男性组=%v", got)
	}
	if got := groups[3].Tags; len(got) != 1 || got[0] != "nobody" {
		t.Fatalf("未翻译标签应保留原文，得到 %v", got)
	}
}
