// Package cli 实现命令行入口、子命令参数解析与分发。
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/usfese/telehentai/internal/app"
	"github.com/usfese/telehentai/internal/config"
	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/imgsrv"
	"github.com/usfese/telehentai/internal/logx"
	"github.com/usfese/telehentai/internal/pipeline"
	"github.com/usfese/telehentai/internal/store"
	"github.com/usfese/telehentai/internal/telegraph"
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
	dataDir      string
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
	case "run":
		o, err := parseRun(args[1:])
		if err != nil {
			return err
		}
		return runBot(ctx, o)
	case "fetch":
		o, err := parseFetch(args[1:])
		if err != nil {
			return ignoreHelp(err)
		}
		return runFetch(ctx, o)
	case "publish":
		o, err := parsePublish(args[1:])
		if err != nil {
			return ignoreHelp(err)
		}
		return runPublish(ctx, o)
	case "serve-images":
		o, err := parseServeImages(args[1:])
		if err != nil {
			return ignoreHelp(err)
		}
		return runServeImages(ctx, o)
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
	fs.StringVar(&o.dataDir, "data-dir", "", "覆盖 download.data_dir")
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

// loadConfig 在用户显式指定 --config 时要求文件存在，否则回落到 ./config.yaml。
// ignoreHelp 让 `--help` 以退出码 0 结束，而不是把 flag.ErrHelp 当成错误打印。
func ignoreHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		return config.Load("config.yaml", false)
	}
	return config.Load(path, true)
}

func runBot(ctx context.Context, o runOptions) error {
	cfg, err := loadConfig(o.configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(config.ModeRun); err != nil {
		return err
	}
	logger := logx.New(cfg.Log.Level, cfg.Log.Format)
	return app.Run(ctx, cfg, logger)
}

func runServeImages(ctx context.Context, o serveImagesOptions) error {
	cfg, err := loadConfig(o.configPath)
	if err != nil {
		return err
	}
	if o.dataDir != "" {
		cfg.Download.DataDir = o.dataDir
	}
	if o.listen != "" {
		cfg.ImageServer.Listen = o.listen
	}
	if err := cfg.Validate(config.ModeServeImages); err != nil {
		return err
	}
	logger := logx.New(cfg.Log.Level, cfg.Log.Format)
	st, err := store.New(cfg.Download.DataDir)
	if err != nil {
		return err
	}
	srv, err := imgsrv.New(app.ImageServerConfig(cfg), st, logger)
	if err != nil {
		return err
	}
	return srv.Run(ctx)
}

func runFetch(ctx context.Context, o fetchOptions) error {
	cfg, err := loadConfig(o.configPath)
	if err != nil {
		return err
	}
	if o.dataDir != "" {
		cfg.Download.DataDir = o.dataDir
	}
	if err := cfg.Validate(config.ModeFetch); err != nil {
		return err
	}
	logger := logx.New(cfg.Log.Level, cfg.Log.Format)

	st, err := store.New(cfg.Download.DataDir)
	if err != nil {
		return err
	}
	src, err := ehentai.New(app.EhentaiConfig(cfg), logger)
	if err != nil {
		return err
	}

	link, err := ehentai.ParseLink(o.url)
	if err != nil {
		return err
	}
	ref, err := src.ResolveRef(ctx, link)
	if err != nil {
		return err
	}
	meta, err := src.Meta(ctx, ref)
	if err != nil {
		return err
	}
	logger.Info("画廊元数据", "gid", ref.GID, "title", meta.Title, "pages", meta.FileCount)

	progress := func(done, total int) {
		if done == total || done%10 == 0 {
			logger.Info("下载进度", "done", done, "total", total)
		}
	}
	stats, err := pipeline.DownloadGallery(ctx, src, st, ref, meta, o.limit, cfg.Pipeline.DownloadConcurrency, progress, logger)
	if err != nil {
		return err
	}
	if stats.UsedFallback {
		logger.Warn("部分图片未取得原图，已使用重采样图")
	}
	fmt.Printf("下载完成：%d 张 -> %s\n", stats.Images, st.GalleryDir(ref.GID))
	if stats.Failed > 0 {
		fmt.Printf("其中 %d 页下载失败（详见日志）\n", stats.Failed)
	}
	return nil
}

func runPublish(ctx context.Context, o publishOptions) error {
	cfg, err := loadConfig(o.configPath)
	if err != nil {
		return err
	}
	if o.dataDir != "" {
		cfg.Download.DataDir = o.dataDir
	}
	if o.imageBaseURL != "" {
		cfg.ImageServer.PublicBaseURL = o.imageBaseURL
	}
	if err := cfg.Validate(config.ModePublish); err != nil {
		return err
	}
	logger := logx.New(cfg.Log.Level, cfg.Log.Format)

	st, err := store.New(cfg.Download.DataDir)
	if err != nil {
		return err
	}
	gid, meta, err := publishTarget(ctx, o.target, cfg, st, logger)
	if err != nil {
		return err
	}
	images, err := st.ListImages(gid)
	if err != nil {
		return err
	}
	if len(images) == 0 {
		return fmt.Errorf("画廊 %d 没有本地图片，请先运行 fetch", gid)
	}
	if o.limit > 0 && len(images) > o.limit {
		images = images[:o.limit]
	}

	in := telegraph.Input{
		GID:       gid,
		Title:     meta.Title,
		Category:  meta.Category,
		SourceURL: meta.URL,
		Total:     meta.FileCount,
		Posted:    meta.Posted,
		Tags:      meta.Tags,
		ImageBase: strings.TrimRight(cfg.ImageServer.PublicBaseURL, "/") + "/i/" + strconv.Itoa(gid),
	}
	pub := telegraph.New(app.TelegraphConfig(cfg), tokenStore{st: st}, logger)
	res, err := pipeline.PublishImages(ctx, app.PipelineConfig(cfg), pub, images, in, logger)
	if err != nil {
		return err
	}
	for _, u := range res.URLs {
		fmt.Println(u)
	}
	fmt.Printf("发布完成：%d 张图，%d 个页面\n", res.Images, len(res.URLs))
	return nil
}

// publishTarget 解析 publish 的位置参数：纯数字按 gid 读取本地元数据；
// 画廊链接在本地缺少元数据时联网现取。
func publishTarget(ctx context.Context, target string, cfg *config.Config, st *store.Store, logger *slog.Logger) (int, ehentai.GalleryMeta, error) {
	if gid, err := strconv.Atoi(strings.TrimSpace(target)); err == nil && gid > 0 {
		meta, err := st.LoadMeta(gid)
		if err != nil {
			return 0, ehentai.GalleryMeta{}, fmt.Errorf("读取 gid=%d 的本地元数据失败，请先运行 fetch: %w", gid, err)
		}
		return gid, meta, nil
	}

	link, err := ehentai.ParseLink(target)
	if err != nil {
		return 0, ehentai.GalleryMeta{}, fmt.Errorf("参数必须是 gid 或画廊链接: %w", err)
	}
	src, err := ehentai.New(app.EhentaiConfig(cfg), logger)
	if err != nil {
		return 0, ehentai.GalleryMeta{}, err
	}
	ref, err := src.ResolveRef(ctx, link)
	if err != nil {
		return 0, ehentai.GalleryMeta{}, err
	}
	if meta, err := st.LoadMeta(ref.GID); err == nil && meta.Title != "" {
		return ref.GID, meta, nil
	}
	meta, err := src.Meta(ctx, ref)
	if err != nil {
		return 0, ehentai.GalleryMeta{}, err
	}
	return ref.GID, meta, nil
}

// tokenStore 让 *store.Store 满足 telegraph.TokenStore。
type tokenStore struct{ st *store.Store }

func (t tokenStore) Load() (string, error) { return t.st.TelegraphToken() }

func (t tokenStore) Save(s string) error { return t.st.SaveTelegraphToken(s) }
