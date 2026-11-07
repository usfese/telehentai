package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/usfese/telehentai/internal/ehentai"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestNewCreatesDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "data")
	s, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "galleries"))
	if err != nil || !info.IsDir() {
		t.Fatalf("galleries 目录未创建: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o027 != 0 {
		t.Fatalf("目录权限过宽: %o", perm)
	}
	if s.Root() != root {
		t.Fatalf("Root=%q", s.Root())
	}
}

func TestSaveAndLoadMeta(t *testing.T) {
	s := newTestStore(t)
	meta := ehentai.GalleryMeta{
		GID:       42,
		Token:     "abcdef1234",
		Title:     "测试画廊",
		FileCount: 3,
		Posted:    time.Unix(1700000000, 0).UTC(),
	}
	if err := s.SaveMeta(42, meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	got, err := s.LoadMeta(42)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if got.GID != meta.GID || got.Title != meta.Title || got.FileCount != meta.FileCount || !got.Posted.Equal(meta.Posted) {
		t.Fatalf("往返失败: %+v", got)
	}
	// 不应残留临时文件。
	entries, _ := os.ReadDir(s.GalleryDir(42))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "meta.json.tmp") {
			t.Fatalf("残留临时文件 %s", e.Name())
		}
	}
}

func TestLoadMetaMissing(t *testing.T) {
	s := newTestStore(t)
	_, err := s.LoadMeta(999)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("期望 os.ErrNotExist，得到 %v", err)
	}
}

func TestSaveImageAtomicAndExists(t *testing.T) {
	s := newTestStore(t)
	name, err := s.SaveImage(7, 3, ".webp", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("SaveImage: %v", err)
	}
	if name != "0003.webp" {
		t.Fatalf("文件名=%q", name)
	}
	data, err := os.ReadFile(s.ImagePath(7, name))
	if err != nil || string(data) != "hello" {
		t.Fatalf("内容错误: %q, %v", data, err)
	}
	info, err := os.Stat(s.ImagePath(7, name))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != filePerm {
		t.Fatalf("文件权限=%o，期望 %o", perm, filePerm)
	}

	ok, got := s.ImageExists(7, 3)
	if !ok || got != "0003.webp" {
		t.Fatalf("ImageExists=(%v,%q)", ok, got)
	}
	if ok, _ := s.ImageExists(7, 4); ok {
		t.Fatal("不存在的页被报告为存在")
	}

	entries, _ := os.ReadDir(s.GalleryDir(7))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("残留临时文件 %s", e.Name())
		}
	}
}

func TestListImagesOrderAndFilter(t *testing.T) {
	s := newTestStore(t)
	dir := s.GalleryDir(9)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0010.jpg", "0002.webp", "0001.png", "notes.txt", "0003.webp.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	names, err := s.ListImages(9)
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	want := []string{"0001.png", "0002.webp", "0010.jpg"}
	if len(names) != len(want) {
		t.Fatalf("得到 %v，期望 %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("顺序错误: %v，期望 %v", names, want)
		}
	}
}

func TestListImagesMissingDir(t *testing.T) {
	s := newTestStore(t)
	names, err := s.ListImages(12345)
	if err != nil {
		t.Fatalf("缺目录应返回空列表，得到 %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("期望空列表，得到 %v", names)
	}
}

func TestHasAll(t *testing.T) {
	s := newTestStore(t)
	if ok, err := s.HasAll(5, 0); err != nil || ok {
		t.Fatalf("want=0 应返回 false,nil，得到 %v,%v", ok, err)
	}
	for page := 1; page <= 3; page++ {
		if _, err := s.SaveImage(5, page, ".jpg", strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := s.HasAll(5, 3); err != nil || !ok {
		t.Fatalf("HasAll(3)=(%v,%v)", ok, err)
	}
	if ok, err := s.HasAll(5, 4); err != nil || ok {
		t.Fatalf("HasAll(4)=(%v,%v)", ok, err)
	}
}

func TestTelegraphTokenRoundTrip(t *testing.T) {
	s := newTestStore(t)
	tok, err := s.TelegraphToken()
	if err != nil || tok != "" {
		t.Fatalf("缺失时应返回空串，得到 %q,%v", tok, err)
	}
	if err := s.SaveTelegraphToken("secret"); err != nil {
		t.Fatalf("SaveTelegraphToken: %v", err)
	}
	got, err := s.TelegraphToken()
	if err != nil || got != "secret" {
		t.Fatalf("读取失败: %q,%v", got, err)
	}
	info, err := os.Stat(filepath.Join(s.Root(), "telegraph.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token 文件权限=%o，期望 600", perm)
	}
}
