// Package store 管理本地画廊缓存：图片、元数据与 Telegraph token。
//
// 目录布局：
//
//	<root>/galleries/<gid>/0001.webp
//	<root>/galleries/<gid>/meta.json
//	<root>/telegraph.json
package store

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"github.com/usfese/telehentai/internal/ehentai"
)

const (
	dirPerm  = 0o750
	filePerm = 0o640
)

// imageNameRe 限定缓存目录里可被识别的图片文件名。
var imageNameRe = regexp.MustCompile(`^\d+\.(webp|jpg|jpeg|png|gif)$`)

// imageExts 是 ImageExists 探测时尝试的扩展名顺序。
var imageExts = []string{".webp", ".jpg", ".png", ".gif"}

// Store 是只依赖文件系统的本地存储；零值不可用，必须用 New 构造。
type Store struct {
	root string
}

// New 创建数据目录（含 galleries 子目录）。
func New(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("store: 数据目录不能为空")
	}
	if err := os.MkdirAll(filepath.Join(root, "galleries"), dirPerm); err != nil {
		return nil, fmt.Errorf("创建数据目录 %s: %w", root, err)
	}
	return &Store{root: root}, nil
}

// Root 返回数据根目录。
func (s *Store) Root() string { return s.root }

// GalleryDir 返回某本画廊的缓存目录。
func (s *Store) GalleryDir(gid int) string {
	return filepath.Join(s.root, "galleries", strconv.Itoa(gid))
}

// ImagePath 返回图片的绝对路径；name 应由调用方校验过。
func (s *Store) ImagePath(gid int, name string) string {
	return filepath.Join(s.GalleryDir(gid), name)
}

// SaveMeta 原子写入 meta.json：先写临时文件再 rename，避免进程中断留下半个 JSON。
func (s *Store) SaveMeta(gid int, m ehentai.GalleryMeta) error {
	dir := s.GalleryDir(gid)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("创建画廊目录 %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化元数据: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "meta.json.tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入临时元数据: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时元数据: %w", err)
	}
	if err := os.Chmod(tmpName, filePerm); err != nil {
		return fmt.Errorf("设置元数据权限: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, "meta.json")); err != nil {
		return fmt.Errorf("提交元数据: %w", err)
	}
	return nil
}

// LoadMeta 读取 meta.json；文件不存在时返回的错误可用 errors.Is(err, os.ErrNotExist) 判断。
func (s *Store) LoadMeta(gid int) (ehentai.GalleryMeta, error) {
	path := filepath.Join(s.GalleryDir(gid), "meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ehentai.GalleryMeta{}, fmt.Errorf("读取元数据 %s: %w", path, err)
	}
	var m ehentai.GalleryMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return ehentai.GalleryMeta{}, fmt.Errorf("解析元数据 %s: %w", path, err)
	}
	return m, nil
}

// ImageExists 按扩展名顺序探测某页是否已下载，返回文件名。
func (s *Store) ImageExists(gid, page int) (bool, string) {
	base := fmt.Sprintf("%04d", page)
	for _, ext := range imageExts {
		name := base + ext
		if info, err := os.Stat(s.ImagePath(gid, name)); err == nil && !info.IsDir() {
			return true, name
		}
	}
	return false, ""
}

// SaveImage 原子保存一页图片，返回文件名。
func (s *Store) SaveImage(gid, page int, ext string, r io.Reader) (string, error) {
	dir := s.GalleryDir(gid)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("创建画廊目录 %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, fmt.Sprintf(".tmp-%04d-*", page))
	if err != nil {
		return "", fmt.Errorf("创建临时图片: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		return "", fmt.Errorf("写入图片: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("关闭临时图片: %w", err)
	}
	if err := os.Chmod(tmpName, filePerm); err != nil {
		return "", fmt.Errorf("设置图片权限: %w", err)
	}
	name := fmt.Sprintf("%04d%s", page, ext)
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return "", fmt.Errorf("提交图片 %s: %w", name, err)
	}
	return name, nil
}

// ListImages 返回画廊内已下载的图片文件名，按页码数值升序。
// 目录不存在视为"还没有下载任何图片"，返回空列表而不是错误。
func (s *Store) ListImages(gid int) ([]string, error) {
	entries, err := os.ReadDir(s.GalleryDir(gid))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取画廊目录: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !imageNameRe.MatchString(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Compare(pageOf(a), pageOf(b))
	})
	return names, nil
}

// pageOf 提取文件名中的页码；正则已保证格式合法。
func pageOf(name string) int {
	dot := 0
	for ; dot < len(name); dot++ {
		if name[dot] == '.' {
			break
		}
	}
	n, _ := strconv.Atoi(name[:dot])
	return n
}

// HasAll 判断已下载图片数是否达到 want；want<=0 时返回 false（无参考意义）。
func (s *Store) HasAll(gid, want int) (bool, error) {
	if want <= 0 {
		return false, nil
	}
	names, err := s.ListImages(gid)
	if err != nil {
		return false, err
	}
	return len(names) >= want, nil
}

// TelegraphToken 读取持久化的 telegra.ph access_token；不存在时返回空串而非错误。
func (s *Store) TelegraphToken() (string, error) {
	path := filepath.Join(s.root, "telegraph.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("读取 %s: %w", path, err)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("解析 %s: %w", path, err)
	}
	return payload.AccessToken, nil
}

// SaveTelegraphToken 原子写入 access_token，权限 0600（等同凭据）。
func (s *Store) SaveTelegraphToken(tok string) error {
	path := filepath.Join(s.root, "telegraph.json")
	data, err := json.Marshal(struct {
		AccessToken string `json:"access_token"`
	}{AccessToken: tok})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.root, ".telegraph-*")
	if err != nil {
		return fmt.Errorf("创建临时 token 文件: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("写入 token: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭 token 文件: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("设置 token 权限: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("提交 token 文件: %w", err)
	}
	return nil
}
