package telegraph

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Input 是一次发布的静态信息；ImageBase 是图片目录前缀，图片按 ImageBase+"/"+name 拼接。
type Input struct {
	GID       int
	Title     string
	Category  string
	SourceURL string
	Total     int
	Posted    time.Time
	Tags      []string
	ImageBase string
}

// Part 是分页后的一页内容。
type Part struct {
	Title  string
	Nodes  []Node
	Images int
}

const (
	// navReserveBytes 给调用方追加的导航节点预留预算，避免加上导航后超限。
	navReserveBytes = 512
	// maxTitleRunes 是标题的字符上限（Telegraph 限制按字符计）。
	maxTitleRunes = 256
	// maxTags 是标签节点展示数量上限，防止单节点过大。
	maxTags = 20
)

// BuildPart 从 names 中尽可能多地构造图片节点，返回本页内容与消耗的图片数量。
// partNo 从 1 开始；预算按节点 JSON 字节数计算，超限时截断而不是报错，
// 只有当第一张图就放不下时才返回 ErrContentTooBig，让调用方降低预算重试。
func BuildPart(in Input, names []string, partNo, budget int) (Part, int, error) {
	if partNo < 1 {
		partNo = 1
	}
	part := Part{Title: partTitle(in, partNo)}
	limit := budget - navReserveBytes

	used := 0
	if partNo == 1 {
		meta := metaNodes(in)
		part.Nodes = append(part.Nodes, meta...)
		for _, n := range meta {
			used += nodeBytes(n)
		}
	}
	if used > limit {
		return Part{}, 0, ErrContentTooBig
	}

	for i, name := range names {
		fig := FigureNode(in.ImageBase + "/" + name)
		size := nodeBytes(fig)
		if used+size > limit {
			if i == 0 {
				return Part{}, 0, ErrContentTooBig
			}
			break
		}
		used += size
		part.Nodes = append(part.Nodes, fig)
		part.Images++
	}
	return part, part.Images, nil
}

// partTitle 生成分页标题：首页用原始标题，后续页追加「（第 N 部分）」。
func partTitle(in Input, partNo int) string {
	base := strings.TrimSpace(in.Title)
	if base == "" {
		base = fmt.Sprintf("gallery %d", in.GID)
	}
	base = truncateRunes(base, maxTitleRunes)
	if partNo <= 1 {
		return base
	}
	suffix := fmt.Sprintf("（第 %d 部分）", partNo)
	return truncateRunes(base, maxTitleRunes-len([]rune(suffix))) + suffix
}

// truncateRunes 按字符（而非字节）截断，避免截断中文产生乱码。
func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

// metaNodes 构造首页开头的元数据节点；空字段直接省略。
func metaNodes(in Input) []Node {
	var nodes []Node

	var parts []string
	if in.Category != "" {
		parts = append(parts, "分类："+in.Category)
	}
	if in.Total > 0 {
		parts = append(parts, fmt.Sprintf("共 %d 页", in.Total))
	}
	if !in.Posted.IsZero() {
		parts = append(parts, "发布于 "+in.Posted.Format("2006-01-02"))
	}
	if len(parts) > 0 {
		nodes = append(nodes, Node{Tag: "p", Children: []any{strings.Join(parts, " · ")}})
	}

	if len(in.Tags) > 0 {
		tags := in.Tags
		if len(tags) > maxTags {
			tags = tags[:maxTags]
		}
		nodes = append(nodes, Node{Tag: "p", Children: []any{"标签：" + strings.Join(tags, "、")}})
	}

	if in.SourceURL != "" {
		nodes = append(nodes, Node{
			Tag:      "p",
			Children: []any{Node{Tag: "a", Attrs: map[string]string{"href": in.SourceURL}, Children: []any{"E-Hentai 原画廊"}}},
		})
	}
	return nodes
}

// NavNode 构造分页导航行；prevURL/nextURL 为空时省略对应链接。
func NavNode(partNo int, prevURL, nextURL string) Node {
	children := []any{fmt.Sprintf("第 %d 部分", partNo)}
	if prevURL != "" {
		children = append(children, " · ", Node{Tag: "a", Attrs: map[string]string{"href": prevURL}, Children: []any{"上一部分"}})
	}
	if nextURL != "" {
		children = append(children, " · ", Node{Tag: "a", Attrs: map[string]string{"href": nextURL}, Children: []any{"下一部分"}})
	}
	return Node{Tag: "p", Children: children}
}

// FigureNode 构造一张图片节点；不写 figcaption 以节省字节预算。
func FigureNode(src string) Node {
	return Node{
		Tag:      "figure",
		Children: []any{Node{Tag: "img", Attrs: map[string]string{"src": src}}},
	}
}

// nodeBytes 返回节点序列化后的字节数。
func nodeBytes(n Node) int {
	b, err := json.Marshal(n)
	if err != nil {
		return 1 << 30 // 不可序列化时视为巨大，调用方会截断
	}
	return len(b)
}
