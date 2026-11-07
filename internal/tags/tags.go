// Package tags 把 E-Hentai 的「命名空间:标签」翻译成中文。
//
// 数据来自 EhTagTranslation/Database（CC BY-NC-SA 3.0），由 scripts/update_tags.py
// 生成 data/tags.json 并随二进制嵌入，运行时不联网。
package tags

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed data/tags.json
var rawDB []byte

type database struct {
	Version    string                       `json:"version"`
	Namespaces map[string]string            `json:"namespaces"`
	Tags       map[string]map[string]string `json:"tags"`
}

var (
	once sync.Once
	db   database
)

func load() *database {
	once.Do(func() {
		if err := json.Unmarshal(rawDB, &db); err != nil {
			// 数据损坏时退化为不翻译，而不是让整个发布失败。
			db = database{}
		}
	})
	return &db
}

// Translate 把 "female:lolicon" 翻译为 "女性：萝莉"。
// 命名空间或标签没有收录时原样返回，保证信息不丢失。
func Translate(tag string) string {
	tag = strings.TrimSpace(tag)
	ns, name, ok := splitTag(tag)
	if !ok {
		return tag
	}
	zh := lookup(ns, name)
	if zh == "" {
		return tag
	}
	if label := load().Namespaces[ns]; label != "" {
		return label + "：" + zh
	}
	return zh
}

// Group 是同一命名空间下的一组标签（Namespace 为中文名，Tags 为翻译后的标签名）。
type Group struct {
	Namespace string
	Tags      []string
}

// namespaceOrder 决定标签行的展示顺序：性别在前，其次作品信息与出处，最后是其他。
var namespaceOrder = []string{
	"female", "male", "mixed", "language", "parody", "character",
	"group", "artist", "cosplayer", "location", "other", "reclass", "temp",
}

// GroupTags 按命名空间归并标签，便于渲染成多行（女性一行、男性一行、语言一行……）。
// 未收录的命名空间保留首次出现的顺序并附在末尾；未收录的标签保留原文。
func GroupTags(list []string) []Group {
	d := load()
	byNS := make(map[string]*Group, 8)
	var extras []string

	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		ns, name, ok := splitTag(raw)
		if !ok {
			ns, name = "", raw
		}
		g, exists := byNS[ns]
		if !exists {
			label := d.Namespaces[ns]
			if label == "" {
				if ns == "" {
					label = "标签"
				} else {
					label = ns
				}
			}
			g = &Group{Namespace: label}
			byNS[ns] = g
			if !knownNamespace(ns) {
				extras = append(extras, ns)
			}
		}
		zh := ""
		if ok {
			zh = lookup(ns, name)
		}
		if zh == "" {
			zh = name
		}
		if zh == "" {
			zh = raw
		}
		g.Tags = append(g.Tags, zh)
	}

	groups := make([]Group, 0, len(byNS))
	for _, ns := range namespaceOrder {
		if g, exists := byNS[ns]; exists {
			groups = append(groups, *g)
			delete(byNS, ns)
		}
	}
	for _, ns := range extras {
		if g, exists := byNS[ns]; exists {
			groups = append(groups, *g)
		}
	}
	return groups
}

func splitTag(tag string) (ns, name string, ok bool) {
	ns, name, ok = strings.Cut(strings.TrimSpace(tag), ":")
	if !ok {
		return "", strings.TrimSpace(tag), false
	}
	return strings.ToLower(strings.TrimSpace(ns)), strings.TrimSpace(name), true
}

func lookup(ns, name string) string {
	table := load().Tags[ns]
	if table == nil {
		return ""
	}
	return table[strings.ToLower(strings.TrimSpace(name))]
}

func knownNamespace(ns string) bool {
	for _, n := range namespaceOrder {
		if n == ns {
			return true
		}
	}
	return false
}

// Version 返回内置数据库版本，用于日志与排查。
func Version() string { return load().Version }
