// Package pipeline 串联「元数据 - 下载 - 发布」三个阶段，并对外提供任务队列。
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/telegraph"
)

// Pipeline 是有界任务队列 + 固定数量 worker 的执行器。
type Pipeline struct {
	cfg    Config
	src    Source
	pub    Publisher
	store  ImageStore
	logger *slog.Logger

	queue    chan *Job
	mu       sync.Mutex
	inflight map[int]struct{}
	wg       sync.WaitGroup
	once     sync.Once
}

// New 构造流水线；QueueSize 与 Workers 至少为 1。
func New(cfg Config, src Source, pub Publisher, st ImageStore, logger *slog.Logger) *Pipeline {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	if cfg.QueueSize < 1 {
		cfg.QueueSize = 1
	}
	if cfg.MaxContentBytes <= 0 {
		cfg.MaxContentBytes = 49152
	}
	if cfg.DownloadConcurrency < 1 {
		cfg.DownloadConcurrency = 4
	}
	return &Pipeline{
		cfg:      cfg,
		src:      src,
		pub:      pub,
		store:    st,
		logger:   logger,
		queue:    make(chan *Job, cfg.QueueSize),
		inflight: make(map[int]struct{}),
	}
}

// Submit 尝试入队。队列已满或同一 gid 正在处理时返回 false。
func (p *Pipeline) Submit(j *Job) bool {
	if j == nil || j.Ref.GID <= 0 {
		return false
	}
	p.mu.Lock()
	if _, busy := p.inflight[j.Ref.GID]; busy {
		p.mu.Unlock()
		return false
	}
	p.inflight[j.Ref.GID] = struct{}{}
	p.mu.Unlock()

	select {
	case p.queue <- j:
		return true
	default:
		p.mu.Lock()
		delete(p.inflight, j.Ref.GID)
		p.mu.Unlock()
		return false
	}
}

// Start 启动 worker；重复调用只生效一次。ctx 取消后 worker 退出。
func (p *Pipeline) Start(ctx context.Context) {
	p.once.Do(func() {
		for range p.cfg.Workers {
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case j := <-p.queue:
						p.run(ctx, j)
					}
				}
			}()
		}
		p.logger.Info("流水线启动", "workers", p.cfg.Workers, "queue_size", p.cfg.QueueSize)
	})
}

func (p *Pipeline) run(ctx context.Context, j *Job) {
	defer func() {
		p.mu.Lock()
		delete(p.inflight, j.Ref.GID)
		p.mu.Unlock()
	}()

	res, err := p.Process(ctx, j)
	if err != nil {
		res = Result{}
		p.logger.Warn("任务失败", "gid", j.Ref.GID, "err", err)
	}
	if j.Done != nil {
		j.Done(res, err)
	}
}

// Process 执行单个任务：解析元数据、按需下载、发布 Telegraph 页面。
func (p *Pipeline) Process(ctx context.Context, j *Job) (Result, error) {
	report := func(pr Progress) {
		if j.Progress != nil {
			j.Progress(pr)
		}
	}

	report(Progress{Stage: StageMeta})
	meta, err := p.src.Meta(ctx, j.Ref)
	if err != nil {
		return Result{}, err
	}

	report(Progress{Stage: StageListing})
	images, err := p.store.ListImages(j.Ref.GID)
	if err != nil {
		return Result{}, err
	}
	cached, err := p.store.HasAll(j.Ref.GID, meta.FileCount)
	if err != nil {
		return Result{}, err
	}

	var stats DownloadStats
	if cached {
		report(Progress{Stage: StageDownloading, Done: len(images), Total: len(images), Note: "缓存命中"})
	} else {
		stats, err = DownloadGallery(ctx, p.src, p.store, j.Ref, meta, 0, p.cfg.DownloadConcurrency, p.throttledReporter(report), p.logger)
		if err != nil {
			return Result{}, err
		}
		if images, err = p.store.ListImages(j.Ref.GID); err != nil {
			return Result{}, err
		}
		if stats.Images == 0 && len(images) == 0 {
			return Result{}, errors.New("没有下载到任何图片")
		}
	}
	if len(images) == 0 {
		return Result{}, errors.New("没有可发布的图片")
	}

	report(Progress{Stage: StagePublishing})
	in := telegraph.Input{
		GID:       j.Ref.GID,
		Title:     meta.Title,
		Category:  meta.Category,
		SourceURL: meta.URL,
		Total:     meta.FileCount,
		Posted:    meta.Posted,
		Tags:      meta.Tags,
		ImageBase: strings.TrimRight(p.cfg.PublicBaseURL, "/") + "/i/" + strconv.Itoa(j.Ref.GID),
	}
	res, err := PublishImages(ctx, p.cfg, p.pub, images, in, p.logger)
	if err != nil {
		return Result{}, err
	}
	res.Failed = stats.Failed
	res.UsedFallback = stats.UsedFallback

	note := ""
	if res.Failed > 0 {
		note = fmt.Sprintf("%d 页下载失败", res.Failed)
	}
	report(Progress{Stage: StageDone, Done: res.Images, Total: res.Images, Note: note})
	return res, nil
}

// throttledReporter 按 ProgressInterval 丢弃中间进度，但每页完成时总能上报。
func (p *Pipeline) throttledReporter(report func(Progress)) func(done, total int) {
	var last time.Time
	return func(done, total int) {
		now := time.Now()
		if p.cfg.ProgressInterval > 0 && now.Sub(last) < p.cfg.ProgressInterval && done < total {
			return
		}
		last = now
		report(Progress{Stage: StageDownloading, Done: done, Total: total})
	}
}

// DownloadGallery 逐页下载画廊并保存到本地；已存在的页直接跳过。
// limit<=0 表示不限；单页失败只记录并继续，结束后仍写入元数据。
func DownloadGallery(ctx context.Context, src Source, st ImageStore, ref ehentai.GalleryRef, meta ehentai.GalleryMeta, limit, concurrency int, onProgress func(done, total int), logger *slog.Logger) (DownloadStats, error) {
	if logger == nil {
		logger = slog.Default()
	}
	var stats DownloadStats

	pages, err := src.ListImagePages(ctx, ref, meta.FileCount)
	if err != nil {
		return stats, err
	}
	total := len(pages)
	if limit > 0 && len(pages) > limit {
		pages = pages[:limit]
		total = limit
	}
	if concurrency < 1 {
		concurrency = 4
	}

	var (
		mu         sync.Mutex
		progressMu sync.Mutex
		processed  int
		wg         sync.WaitGroup
	)
	sem := make(chan struct{}, concurrency)

loop:
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(page ehentai.ImagePage) {
			defer wg.Done()
			defer func() { <-sem }()

			exists, _ := st.ImageExists(ref.GID, page.Page)
			if !exists {
				data, err := downloadPage(ctx, src, st, ref.GID, page, logger)
				mu.Lock()
				if err != nil {
					stats.Failed++
				} else {
					stats.Images++
					if data.UsedFallback {
						stats.UsedFallback = true
					}
				}
				mu.Unlock()
				if err != nil {
					logger.Warn("下载图片失败", "gid", ref.GID, "page", page.Page, "err", err)
				}
			}

			mu.Lock()
			processed++
			done := processed
			mu.Unlock()

			if onProgress != nil {
				// 进度回调可能被多个 worker 并发调用，这里串行化，避免上层节流状态竞争。
				progressMu.Lock()
				onProgress(done, total)
				progressMu.Unlock()
			}
		}(page)
	}
	wg.Wait()

	if err := st.SaveMeta(ref.GID, meta); err != nil {
		return stats, fmt.Errorf("保存元数据: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	return stats, nil
}

// downloadPage 下载并保存单页；失败时重新解析页面再试一次，
// 因为 H@H 的 keystamp 有时效性，重试通常需要新的图片地址。
func downloadPage(ctx context.Context, src Source, st ImageStore, gid int, page ehentai.ImagePage, logger *slog.Logger) (ehentai.ImageData, error) {
	var lastErr error
	for attempt := range 2 {
		if attempt > 0 {
			logger.Debug("重试图片下载", "gid", gid, "page", page.Page)
		}
		ref, err := src.ResolveImage(ctx, page)
		if err != nil {
			lastErr = err
			continue
		}
		rc, data, err := src.Open(ctx, ref)
		if err != nil {
			lastErr = err
			continue
		}
		_, saveErr := st.SaveImage(gid, page.Page, data.Ext, rc)
		closeErr := rc.Close()
		if saveErr != nil {
			lastErr = saveErr
			continue
		}
		if closeErr != nil {
			lastErr = closeErr
			continue
		}
		return data, nil
	}
	if lastErr == nil {
		lastErr = errors.New("图片下载失败")
	}
	return ehentai.ImageData{}, lastErr
}

// PublishImages 把图片按预算切分成多页发布，并为相邻页面补齐导航链接。
// 内容超限时把预算按 0.6 倍回退重试同一页。
func PublishImages(ctx context.Context, cfg Config, pub Publisher, images []string, in telegraph.Input, logger *slog.Logger) (Result, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(images) == 0 {
		return Result{}, errors.New("没有可发布的图片")
	}
	// 文章里的「共 N 页」= 实际发布张数。
	in.Total = len(images)

	type createdPart struct {
		part    telegraph.Part
		partNo  int
		page    telegraph.Page
		prevURL string
	}

	budget := cfg.MaxContentBytes
	partNo := 1
	idx := 0
	prevURL := ""
	var prev *createdPart
	urls := make([]string, 0, 4)

	for idx < len(images) {
		part, n, err := telegraph.BuildPart(in, images[idx:], partNo, budget)
		if err != nil {
			return Result{}, err
		}
		if n <= 0 {
			return Result{}, errors.New("分页失败：本次没有消耗任何图片")
		}

		nodes := append(append([]telegraph.Node{}, part.Nodes...), telegraph.NavNode(partNo, prevURL, ""))
		page, err := pub.CreatePage(ctx, telegraph.PageInput{
			Title:      part.Title,
			Content:    nodes,
			AuthorName: cfg.AuthorName,
			AuthorURL:  cfg.AuthorURL,
		})
		if errors.Is(err, telegraph.ErrContentTooBig) {
			budget = budget * 6 / 10
			if budget < 4096 {
				return Result{}, err
			}
			logger.Warn("Telegraph 内容超限，降低预算后重试", "budget", budget)
			continue // 同一 idx、同一 partNo 重试
		}
		if err != nil {
			return Result{}, err
		}

		if prev != nil {
			prevNodes := append(append([]telegraph.Node{}, prev.part.Nodes...), telegraph.NavNode(prev.partNo, prev.prevURL, page.URL))
			if _, err := pub.EditPage(ctx, prev.page.Path, telegraph.PageInput{
				Title:      prev.part.Title,
				Content:    prevNodes,
				AuthorName: cfg.AuthorName,
				AuthorURL:  cfg.AuthorURL,
			}); err != nil {
				logger.Warn("补充下一页链接失败", "path", prev.page.Path, "err", err)
			}
		}

		urls = append(urls, page.URL)
		prev = &createdPart{part: part, partNo: partNo, page: page, prevURL: prevURL}
		prevURL = page.URL
		idx += n
		partNo++
	}

	return Result{URLs: urls, Images: len(images)}, nil
}
