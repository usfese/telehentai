// Package cli 实现命令行入口、子命令参数解析与分发。
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

// Version 是二进制版本号，可在构建时用 -ldflags "-X ...cli.Version=..." 覆盖。
var Version = "dev"

// runOptions 对应 `telehentai run`。
type runOptions struct {
	configPath string
}

// fetchOptions 对应 `telehentai fetch`。
type fetchOptions struct {
	configPath string
	dataDir    string
	limit      int
	url        string
}

// publishOptions 对应 `telehentai publish`。
type publishOptions struct {
	configPath   string
	imageBaseURL string
	limit        int
	target       string
}

// serveImagesOptions 对应 `telehentai serve-images`。
type serveImagesOptions struct {
	configPath string
	listen     string
	dataDir    string
}

const usageText = `telehentai —— E-Hentai 画廊转 Telegraph 图集

用法：
  telehentai run          [--config PATH]
  telehentai fetch        [--config PATH] [--data-dir DIR] [--limit N] <链接>
  telehentai publish      [--config PATH] [--image-base-url URL] [--limit N] <gid|链接>
  telehentai serve-images [--config PATH] [--listen ADDR] [--data-dir DIR]
  telehentai version

注意：flag 必须写在位置参数之前（标准库 flag 的解析规则）。
`

// Execute 解析参数并执行子命令；返回非 nil 时由 main 以退出码 1 结束。
func Execute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(os.Stderr, usageText)
		return fmt.Errorf("缺少子命令")
	}
	switch args[0] {
	case "version", "--version", "-v":
		_, _ = fmt.Fprintf(os.Stdout, "telehentai %s\n", Version)
		return nil
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(os.Stdout, usageText)
		return nil
	default:
		_, _ = fmt.Fprint(os.Stderr, usageText)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func newFlagSet(name string, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	return fs
}

func parseRun(args []string) (runOptions, error) {
	var o runOptions
	fs := newFlagSet("run", os.Stderr)
	fs.StringVar(&o.configPath, "config", "", "配置文件路径（默认 ./config.yaml）")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("run 不接受位置参数，得到 %q", fs.Args())
	}
	return o, nil
}

func parseFetch(args []string) (fetchOptions, error) {
	var o fetchOptions
	fs := newFlagSet("fetch", os.Stderr)
	fs.StringVar(&o.configPath, "config", "", "配置文件路径（默认 ./config.yaml）")
	fs.StringVar(&o.dataDir, "data-dir", "", "覆盖 download.data_dir")
	fs.IntVar(&o.limit, "limit", 0, "最多下载多少页（0 表示不限）")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 1 {
		return o, fmt.Errorf("fetch 需要一个画廊链接作为位置参数")
	}
	o.url = fs.Arg(0)
	return o, nil
}

func parsePublish(args []string) (publishOptions, error) {
	var o publishOptions
	fs := newFlagSet("publish", os.Stderr)
	fs.StringVar(&o.configPath, "config", "", "配置文件路径（默认 ./config.yaml）")
	fs.StringVar(&o.imageBaseURL, "image-base-url", "", "覆盖 image_server.public_base_url")
	fs.IntVar(&o.limit, "limit", 0, "最多发布多少张图（0 表示不限）")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 1 {
		return o, fmt.Errorf("publish 需要一个 gid 或画廊链接作为位置参数")
	}
	o.target = fs.Arg(0)
	return o, nil
}

func parseServeImages(args []string) (serveImagesOptions, error) {
	var o serveImagesOptions
	fs := newFlagSet("serve-images", os.Stderr)
	fs.StringVar(&o.configPath, "config", "", "配置文件路径（默认 ./config.yaml）")
	fs.StringVar(&o.listen, "listen", "", "覆盖 image_server.listen")
	fs.StringVar(&o.dataDir, "data-dir", "", "覆盖 download.data_dir")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("serve-images 不接受位置参数，得到 %q", fs.Args())
	}
	return o, nil
}
