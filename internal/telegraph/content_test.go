package telegraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testInput() Input {
	return Input{
		GID:       4310985,
		Title:     "测试画廊",
		Category:  "Doujinshi",
		SourceURL: "https://e-hentai.org/g/4310985/ad9552b3e4/",
		Total:     200,
		Posted:    time.Date(2024, 9, 6, 0, 0, 0, 0, time.UTC),
		Tags:      []string{"tag1", "tag2"},
		ImageBase: "https://img.example.com/i/4310985",
	}
}

func namesFor(n int) []string {
	names := make([]string, n)
	for i := range n {
		names[i] = fmt.Sprintf("%04d.webp", i+1)
	}
	return names
}

func TestBuildPartBudget(t *testing.T) {
	in := testInput()
	names := namesFor(200)
	const budget = 8192

	totalImages := 0
	partNo := 1
	for idx := 0; idx < len(names); {
		part, n, err := BuildPart(in, names[idx:], partNo, budget)
		if err != nil {
			t.Fatalf("第 %d 部分: %v", partNo, err)
		}
		if n <= 0 {
			t.Fatalf("第 %d 部分没有消耗图片，会死循环", partNo)
		}
		nodes := append(append([]Node{}, part.Nodes...), NavNode(partNo, "", ""))
		b, err := json.Marshal(nodes)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > budget {
			t.Fatalf("第 %d 部分 JSON %d 字节超过预算 %d", partNo, len(b), budget)
		}
		totalImages += part.Images
		idx += n
		partNo++
	}
	if totalImages != len(names) {
		t.Fatalf("图片总数不守恒: %d != %d", totalImages, len(names))
	}
	if partNo < 3 {
		t.Fatalf("预算 8192 应切分为多部分，实际 %d 部分", partNo-1)
	}
}

func TestBuildPartFirstImageTooBig(t *testing.T) {
	in := testInput()
	_, _, err := BuildPart(in, namesFor(1), 1, 800)
	if !errors.Is(err, ErrContentTooBig) {
		t.Fatalf("期望 ErrContentTooBig，得到 %v", err)
	}
}

func TestBuildPartTitles(t *testing.T) {
	in := testInput()
	p1, _, err := BuildPart(in, namesFor(1), 1, 49152)
	if err != nil {
		t.Fatal(err)
	}
	if p1.Title != in.Title {
		t.Fatalf("首页标题=%q", p1.Title)
	}
	p2, _, err := BuildPart(in, namesFor(1), 2, 49152)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p2.Title, "第 2 部分") {
		t.Fatalf("第 2 部分标题缺少后缀: %q", p2.Title)
	}

	long := strings.Repeat("长", 400)
	in.Title = long
	for _, partNo := range []int{1, 2} {
		p, _, err := BuildPart(in, namesFor(1), partNo, 49152)
		if err != nil {
			t.Fatal(err)
		}
		if n := len([]rune(p.Title)); n > maxTitleRunes {
			t.Fatalf("第 %d 部分标题 %d 字符超过上限", partNo, n)
		}
	}

	in.Title = "   "
	p, _, err := BuildPart(in, namesFor(1), 1, 49152)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Title, "4310985") {
		t.Fatalf("空标题应回落到 gallery <gid>，得到 %q", p.Title)
	}
}

func TestBuildPartMetaNodes(t *testing.T) {
	in := testInput()
	part, _, err := BuildPart(in, namesFor(2), 1, 49152)
	if err != nil {
		t.Fatal(err)
	}
	// 首页应有 2 张图 + 3 个元数据节点，且图片在前、信息在最后。
	if len(part.Nodes) != 5 {
		t.Fatalf("节点数=%d: %+v", len(part.Nodes), part.Nodes)
	}
	for i := range 2 {
		b, _ := json.Marshal(part.Nodes[i])
		if !strings.Contains(string(b), `"tag":"figure"`) {
			t.Fatalf("第 %d 个节点应为图片: %s", i, b)
		}
	}
	b, _ := json.Marshal(part.Nodes[2])
	if !strings.Contains(string(b), "分类：Doujinshi") || !strings.Contains(string(b), "共 200 页") {
		t.Fatalf("元数据行错误: %s", b)
	}
	last, _ := json.Marshal(part.Nodes[len(part.Nodes)-1])
	if !strings.Contains(string(last), "E-Hentai 原画廊") {
		t.Fatalf("最后应是来源信息: %s", last)
	}

	in.Category = ""
	in.Posted = time.Time{}
	in.Tags = nil
	in.SourceURL = ""
	in.Total = 0
	part, _, err = BuildPart(in, namesFor(1), 1, 49152)
	if err != nil {
		t.Fatal(err)
	}
	if len(part.Nodes) != 1 {
		t.Fatalf("空元数据不应生成节点，得到 %d 个节点", len(part.Nodes))
	}
}

func TestNavNode(t *testing.T) {
	n := NavNode(3, "https://telegra.ph/a", "")
	b, _ := json.Marshal(n)
	if !strings.Contains(string(b), "上一部分") || strings.Contains(string(b), "下一部分") {
		t.Fatalf("只有 prev 时导航错误: %s", b)
	}

	n = NavNode(3, "", "https://telegra.ph/c")
	b, _ = json.Marshal(n)
	if strings.Contains(string(b), "上一部分") || !strings.Contains(string(b), "下一部分") {
		t.Fatalf("只有 next 时导航错误: %s", b)
	}

	n = NavNode(1, "", "")
	b, _ = json.Marshal(n)
	if !strings.Contains(string(b), "第 1 部分") {
		t.Fatalf("缺少部分编号: %s", b)
	}
}

func TestFigureNode(t *testing.T) {
	b, _ := json.Marshal(FigureNode("https://img.example.com/i/1/0001.webp"))
	if !strings.Contains(string(b), `"tag":"figure"`) || !strings.Contains(string(b), `"src":"https://img.example.com/i/1/0001.webp"`) {
		t.Fatalf("figure 节点错误: %s", b)
	}
}
