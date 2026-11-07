package ehentai

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Site 是 E-Hentai 的站点域。
type Site string

const (
	// SiteEHentai 是公开站点 e-hentai.org。
	SiteEHentai Site = "e-hentai"
	// SiteExHentai 是表里站 exhentai.org（Sad Panda）。
	SiteExHentai Site = "exhentai"
)

// Host 返回该站点在 URL 中使用的规范域名。
func (s Site) Host() string {
	if s == SiteExHentai {
		return "exhentai.org"
	}
	return "e-hentai.org"
}

// BaseURL 返回站点根地址（https://<host>）。
func (s Site) BaseURL() string { return "https://" + s.Host() }

// Link 是解析后的画廊或图片页链接。
// 画廊链接填 GID+Token；单页链接额外填 PageToken+Page。
type Link struct {
	Site      Site
	GID       int
	Token     string
	PageToken string
	Page      int
}

// GalleryRef 是下载一本画廊所需的最小标识。
type GalleryRef struct {
	Site  Site
	GID   int
	Token string
}

var (
	galleryPathRe = regexp.MustCompile(`^/g/(\d+)/([0-9a-z]{8,16})/?$`)
	imagePageRe   = regexp.MustCompile(`^/s/([0-9a-z]{8,16})/(\d+)-(\d+)/?$`)
)

// ParseLink 解析画廊链接（/g/<gid>/<token>）或单页链接（/s/<pagekey>/<gid>-<n>）。
// 纯函数、不联网；无法识别时返回包裹 ErrNotGalleryLink 的错误。
func ParseLink(raw string) (Link, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, fmt.Errorf("%w: 无法解析 URL: %v", ErrNotGalleryLink, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Link{}, fmt.Errorf("%w: 只支持 http/https 链接", ErrNotGalleryLink)
	}

	var site Site
	switch strings.ToLower(u.Host) {
	case "e-hentai.org", "www.e-hentai.org":
		site = SiteEHentai
	case "exhentai.org", "www.exhentai.org":
		site = SiteExHentai
	default:
		return Link{}, fmt.Errorf("%w: 不支持的站点 %q", ErrNotGalleryLink, u.Host)
	}

	if m := galleryPathRe.FindStringSubmatch(u.Path); m != nil {
		gid, err := strconv.Atoi(m[1])
		if err != nil || gid <= 0 {
			return Link{}, fmt.Errorf("%w: gid 非法 %q", ErrNotGalleryLink, m[1])
		}
		return Link{Site: site, GID: gid, Token: m[2]}, nil
	}

	// 单页链接带页码，但仍按整本画廊处理；Token 需后续通过 gtoken 接口换取。
	if m := imagePageRe.FindStringSubmatch(u.Path); m != nil {
		gid, err := strconv.Atoi(m[2])
		if err != nil || gid <= 0 {
			return Link{}, fmt.Errorf("%w: gid 非法 %q", ErrNotGalleryLink, m[2])
		}
		page, err := strconv.Atoi(m[3])
		if err != nil || page <= 0 {
			return Link{}, fmt.Errorf("%w: 页码非法 %q", ErrNotGalleryLink, m[3])
		}
		return Link{Site: site, GID: gid, PageToken: m[1], Page: page}, nil
	}

	return Link{}, fmt.Errorf("%w: 无法识别的路径 %q", ErrNotGalleryLink, u.Path)
}

// galleryURL 拼出画廊首页地址。
func galleryURL(ref GalleryRef) string {
	return fmt.Sprintf("%s/g/%d/%s/", ref.Site.BaseURL(), ref.GID, ref.Token)
}
