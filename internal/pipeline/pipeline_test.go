package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/telegraph"
)

type fakeStore struct {
	mu       sync.Mutex
	images   map[int][]string
	meta     map[int]ehentai.GalleryMeta
	listCall int
}

func newFakeStore() *fakeStore {
	return &fakeStore{images: map[int][]string{}, meta: map[int]ehentai.GalleryMeta{}}
}

func (f *fakeStore) HasAll(gid, want int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if want <= 0 {
		return false, nil
	}
	return len(f.images[gid]) >= want, nil
}

func (f *fakeStore) ListImages(gid int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCall++
	return append([]string(nil), f.images[gid]...), nil
}

func (f *fakeStore) ImageExists(gid, page int) (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := fmt.Sprintf("%04d.", page)
	for _, n := range f.images[gid] {
		if strings.HasPrefix(n, prefix) {
			return true, n
		}
	}
	return false, ""
}

func (f *fakeStore) SaveImage(gid, page int, ext string, r io.Reader) (string, error) {
	_, _ = io.Copy(io.Discard, r)
	name := fmt.Sprintf("%04d%s", page, ext)
	f.mu.Lock()
	f.images[gid] = append(f.images[gid], name)
	f.mu.Unlock()
	return name, nil
}

func (f *fakeStore) SaveMeta(gid int, m ehentai.GalleryMeta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.meta[gid] = m
	return nil
}

type fakeSource struct {
	meta         ehentai.GalleryMeta
	pages        []ehentai.ImagePage
	failPages    map[int]bool
	listCalls    atomic.Int32
	resolveCalls atomic.Int32
	openCalls    atomic.Int32
	openDelay    time.Duration
	active       atomic.Int32
	maxActive    atomic.Int32
}

func newFakeSource(n int) *fakeSource {
	pages := make([]ehentai.ImagePage, n)
	for i := range n {
		pages[i] = ehentai.ImagePage{Page: i + 1, URL: fmt.Sprintf("https://e-hentai.org/s/key/%d-%d", 4310985, i+1)}
	}
	return &fakeSource{
		meta:      ehentai.GalleryMeta{GID: 4310985, Title: "测试画廊", FileCount: n, URL: "https://e-hentai.org/g/4310985/ad9552b3e4/"},
		pages:     pages,
		failPages: map[int]bool{},
	}
}

func (f *fakeSource) Meta(context.Context, ehentai.GalleryRef) (ehentai.GalleryMeta, error) {
	return f.meta, nil
}

func (f *fakeSource) ListImagePages(context.Context, ehentai.GalleryRef, int) ([]ehentai.ImagePage, error) {
	f.listCalls.Add(1)
	return f.pages, nil
}

func (f *fakeSource) ResolveImage(_ context.Context, p ehentai.ImagePage) (ehentai.ImageRef, error) {
	f.resolveCalls.Add(1)
	return ehentai.ImageRef{Page: p.Page, ImageURL: "https://img.example.com/src.webp", Referer: p.URL}, nil
}

func (f *fakeSource) Open(_ context.Context, ref ehentai.ImageRef) (io.ReadCloser, ehentai.ImageData, error) {
	f.openCalls.Add(1)
	if f.openDelay > 0 {
		n := f.active.Add(1)
		for {
			m := f.maxActive.Load()
			if n <= m || f.maxActive.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(f.openDelay)
		f.active.Add(-1)
	}
	if f.failPages[ref.Page] {
		return nil, ehentai.ImageData{}, errors.New("模拟下载失败")
	}
	return io.NopCloser(strings.NewReader("image-bytes")), ehentai.ImageData{ContentType: "image/webp", Ext: ".webp"}, nil
}

type fakePublisher struct {
	createCalls   int
	editCalls     int
	failFirst     error
	createdInputs []telegraph.PageInput
	editedInputs  []telegraph.PageInput
}

func (f *fakePublisher) CreatePage(_ context.Context, in telegraph.PageInput) (telegraph.Page, error) {
	f.createCalls++
	f.createdInputs = append(f.createdInputs, in)
	if f.failFirst != nil && f.createCalls == 1 {
		return telegraph.Page{}, f.failFirst
	}
	path := fmt.Sprintf("page-%d", f.createCalls)
	return telegraph.Page{Path: path, URL: "https://telegra.ph/" + path, Title: in.Title}, nil
}

func (f *fakePublisher) EditPage(_ context.Context, path string, in telegraph.PageInput) (telegraph.Page, error) {
	f.editCalls++
	f.editedInputs = append(f.editedInputs, in)
	return telegraph.Page{Path: path, URL: "https://telegra.ph/" + path}, nil
}

func testConfig() Config {
	return Config{
		Workers:          1,
		QueueSize:        2,
		ProgressInterval: 0,
		MaxContentBytes:  49152,
		PublicBaseURL:    "https://img.example.com",
		AuthorName:       "telehentai",
	}
}

func testJob(gid int) *Job {
	return &Job{
		ChatID: 1,
		Ref:    ehentai.GalleryRef{Site: ehentai.SiteEHentai, GID: gid, Token: "ad9552b3e4"},
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProcessHappyPath(t *testing.T) {
	src := newFakeSource(3)
	st := newFakeStore()
	pub := &fakePublisher{}
	p := New(testConfig(), src, pub, st, discardLogger())

	res, err := p.Process(context.Background(), testJob(4310985))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if res.Images != 3 || len(res.URLs) != 1 || res.Failed != 0 {
		t.Fatalf("结果错误: %+v", res)
	}
	if pub.createCalls != 1 {
		t.Fatalf("应创建 1 页，实际 %d", pub.createCalls)
	}
	if got := len(st.images[4310985]); got != 3 {
		t.Fatalf("保存图片数=%d", got)
	}
	if _, ok := st.meta[4310985]; !ok {
		t.Fatal("元数据未保存")
	}
	raw, err := json.Marshal(pub.createdInputs[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, want := range []string{
		"https://img.example.com/i/4310985/0001.webp",
		"https://img.example.com/i/4310985/0003.webp",
		`"tag":"figure"`,
		"E-Hentai 原画廊",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("页面内容缺少 %q: %s", want, content)
		}
	}
}

func TestProcessCacheHitSkipsDownload(t *testing.T) {
	src := newFakeSource(3)
	st := newFakeStore()
	for page := 1; page <= 3; page++ {
		if _, err := st.SaveImage(4310985, page, ".webp", strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	pub := &fakePublisher{}
	p := New(testConfig(), src, pub, st, discardLogger())

	res, err := p.Process(context.Background(), testJob(4310985))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if res.Images != 3 {
		t.Fatalf("应发布缓存图片，得到 %+v", res)
	}
	if src.listCalls.Load() != 0 || src.openCalls.Load() != 0 || src.resolveCalls.Load() != 0 {
		t.Fatalf("缓存命中仍访问了数据源: list=%d open=%d resolve=%d", src.listCalls.Load(), src.openCalls.Load(), src.resolveCalls.Load())
	}
}

func TestPublishBudgetFallback(t *testing.T) {
	src := newFakeSource(3)
	st := newFakeStore()
	pub := &fakePublisher{failFirst: telegraph.ErrContentTooBig}
	p := New(testConfig(), src, pub, st, discardLogger())

	res, err := p.Process(context.Background(), testJob(4310985))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if pub.createCalls != 2 {
		t.Fatalf("内容超限后应重试一次（2 次 CreatePage），实际 %d", pub.createCalls)
	}
	if len(res.URLs) != 1 || res.Images != 3 {
		t.Fatalf("结果错误: %+v", res)
	}
}

func TestProcessPartialDownloadFailure(t *testing.T) {
	src := newFakeSource(3)
	src.failPages[2] = true
	st := newFakeStore()
	pub := &fakePublisher{}
	p := New(testConfig(), src, pub, st, discardLogger())

	res, err := p.Process(context.Background(), testJob(4310985))
	if err != nil {
		t.Fatalf("Process 不应因单页失败而失败: %v", err)
	}
	if res.Failed != 1 {
		t.Fatalf("失败页数=%d，期望 1", res.Failed)
	}
	if res.Images != 2 || len(st.images[4310985]) != 2 {
		t.Fatalf("应发布 2 张，得到 %+v / %v", res, st.images[4310985])
	}
	// 失败页会重新解析并重试一次：1、2(失败两次)、3 => 4 次解析。
	if got := src.resolveCalls.Load(); got != 4 {
		t.Fatalf("ResolveImage 调用=%d，期望 4", got)
	}
}

func TestDownloadGalleryRunsConcurrently(t *testing.T) {
	src := newFakeSource(12)
	src.openDelay = 25 * time.Millisecond
	st := newFakeStore()

	stats, err := DownloadGallery(context.Background(), src, st, testJob(4310985).Ref, src.meta, 0, 4, nil, discardLogger())
	if err != nil {
		t.Fatalf("DownloadGallery: %v", err)
	}
	if stats.Images != 12 {
		t.Fatalf("下载数=%d", stats.Images)
	}
	if got := src.maxActive.Load(); got < 2 {
		t.Fatalf("并发未生效，最大并发=%d", got)
	}
	if got := len(st.images[4310985]); got != 12 {
		t.Fatalf("保存数=%d", got)
	}
}

func TestSubmitDedupAndRelease(t *testing.T) {
	src := newFakeSource(1)
	st := newFakeStore()
	pub := &fakePublisher{}
	p := New(testConfig(), src, pub, st, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	done := make(chan struct{}, 2)
	job := testJob(42)
	job.Done = func(Result, error) { done <- struct{}{} }

	if !p.Submit(job) {
		t.Fatal("首次 Submit 应成功")
	}
	if p.Submit(job) {
		t.Fatal("同一 gid 处理中不应重复入队")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("任务未完成")
	}

	// 任务结束后同一 gid 应可再次提交。
	if !p.Submit(job) {
		t.Fatal("任务结束后应可重新提交")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("第二次任务未完成")
	}
}
