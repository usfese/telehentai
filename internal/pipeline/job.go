package pipeline

import (
	"context"
	"io"
	"time"

	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/telegraph"
)

// Stage 是任务所处阶段，用于生成进度文案。
type Stage int

const (
	// StageQueued 表示任务已入队。
	StageQueued Stage = iota
	// StageMeta 表示正在解析元数据。
	StageMeta
	// StageListing 表示正在获取图片列表。
	StageListing
	// StageDownloading 表示正在下载图片。
	StageDownloading
	// StagePublishing 表示正在生成 Telegraph 页面。
	StagePublishing
	// StageDone 表示全部完成。
	StageDone
	// StageFailed 表示失败。
	StageFailed
)

// String 返回阶段名，便于日志排查。
func (s Stage) String() string {
	switch s {
	case StageQueued:
		return "queued"
	case StageMeta:
		return "meta"
	case StageListing:
		return "listing"
	case StageDownloading:
		return "downloading"
	case StagePublishing:
		return "publishing"
	case StageDone:
		return "done"
	case StageFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Progress 是一次进度上报；Done/Total 仅在下载阶段有意义。
type Progress struct {
	Stage Stage
	Done  int
	Total int
	Note  string
}

// Result 是任务成功后的汇总。
type Result struct {
	// URLs 是按顺序生成的 Telegraph 页面地址。
	URLs []string
	// Images 是实际发布的图片张数。
	Images int
	// UsedFallback 表示有图片使用的是重采样图而非原图。
	UsedFallback bool
	// Failed 是下载失败、被跳过的图片页数。
	Failed int
}

// Job 是一次机器人任务的上下文。
type Job struct {
	ID              string
	ChatID          int64
	StatusMessageID int
	Ref             ehentai.GalleryRef
	// Progress 可为 nil；调用方负责节流与错误容忍。
	Progress func(Progress)
	// Done 在 Process 返回后必定被调用一次（成功或失败）。
	Done func(Result, error)
}

// Config 是流水线自身需要的配置子集，由 main 从 config.Config 映射。
type Config struct {
	Workers          int
	QueueSize        int
	ProgressInterval time.Duration
	MaxContentBytes  int
	// PublicBaseURL 是图床根地址；Process 内部拼成 <base>/i/<gid>。
	PublicBaseURL string
	AuthorName    string
	AuthorURL     string
}

// Source 是 E-Hentai 数据源接口，便于测试替换。
type Source interface {
	Meta(context.Context, ehentai.GalleryRef) (ehentai.GalleryMeta, error)
	ListImagePages(context.Context, ehentai.GalleryRef, int) ([]ehentai.ImagePage, error)
	ResolveImage(context.Context, ehentai.ImagePage) (ehentai.ImageRef, error)
	Open(context.Context, ehentai.ImageRef) (io.ReadCloser, ehentai.ImageData, error)
}

// Publisher 是 Telegraph 发布接口。
type Publisher interface {
	CreatePage(context.Context, telegraph.PageInput) (telegraph.Page, error)
	EditPage(context.Context, string, telegraph.PageInput) (telegraph.Page, error)
}

// ImageStore 是流水线需要的本地存储接口。
type ImageStore interface {
	HasAll(gid, want int) (bool, error)
	ListImages(gid int) ([]string, error)
	ImageExists(gid, page int) (bool, string)
	SaveImage(gid, page int, ext string, r io.Reader) (string, error)
	SaveMeta(gid int, m ehentai.GalleryMeta) error
}

// DownloadStats 汇总一次批量下载。
type DownloadStats struct {
	Images       int
	Failed       int
	UsedFallback bool
}
