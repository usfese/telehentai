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
	ns, name, ok := strings.Cut(tag, ":")
	if !ok {
		return tag
	}
	d := load()
	table := d.Tags[strings.ToLower(ns)]
	if table == nil {
		return tag
	}
	zh := table[strings.ToLower(strings.TrimSpace(name))]
	if zh == "" {
		return tag
	}
	if nsName := d.Namespaces[strings.ToLower(ns)]; nsName != "" {
		return nsName + "：" + zh
	}
	return zh
}

// Version 返回内置数据库版本，用于日志与排查。
func Version() string { return load().Version }
