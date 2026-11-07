// Package ehentai 实现 E-Hentai / ExHentai 的链接解析、元数据、图片列表与图片下载。
//
// 设计要点：
//   - 所有出站请求经过 Pacer 节流并带随机抖动，降低触发风控的概率；
//   - Cookie 只附加给 E-Hentai 域，绝不泄漏给 H@H 等图片 CDN；
//   - 原图下载失败（未登录必然 302 到登录页）时自动回退到重采样图。
package ehentai

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultUserAgent 与桌面浏览器一致，避免站点返回精简页面。
	defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	apiEndpoint = "https://api.e-hentai.org/api.php"

	maxHTMLBytes  = 8 << 20  // 画廊页/图片页 HTML 上限
	maxAPIBytes   = 4 << 20  // API JSON 上限
	maxImageBytes = 64 << 20 // 单张图片上限

	// imagesPerPage 是缩略图页的固定行数，用于估算分页迭代上限。
	imagesPerPage = 20
)

// Config 是 E-Hentai 客户端的独立配置，避免与 config 包耦合。
type Config struct {
	Cookie             string
	UserAgent          string
	Proxy              string
	RequestInterval    time.Duration
	RequestJitter      time.Duration
	BandwidthKiBPerSec int
	Timeout            time.Duration
	Retries            int
	PreferOriginal     bool
	MaxPages           int
}

// GalleryMeta 是发布所需的最小元数据集合。
type GalleryMeta struct {
	GID       int
	Token     string
	Title     string
	TitleJPN  string
	Category  string
	Uploader  string
	FileCount int
	FileSize  int64
	Posted    time.Time
	Rating    float64
	Tags      []string
	Thumb     string
	URL       string
}

// ImagePage 是一张图片在画廊中的位置与单页链接。
type ImagePage struct {
	Page int
	URL  string
}

// ImageRef 是下载单张图片所需的候选地址。
type ImageRef struct {
	Page        int
	OriginalURL string
	ImageURL    string
	Referer     string
}

// ImageData 描述一次成功下载的图片元信息。
type ImageData struct {
	ContentType string
	Ext         string
	Size        int64
	// UsedFallback 为 true 表示实际下载的是重采样图而不是原图。
	UsedFallback bool
}

// Client 是 E-Hentai 访问入口；并发安全。
type Client struct {
	cfg     Config
	logger  *slog.Logger
	pacer   *Pacer
	ua      string
	http    *http.Client // 元数据/HTML 请求，带整体超时
	imgHTTP *http.Client // 图片下载，不设整体超时（限速会拉长耗时）
}

// New 构造客户端。代理支持 http(s):// 与 socks5://。
func New(cfg Config, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.RequestInterval <= 0 {
		cfg.RequestInterval = time.Second
	}
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = 5000
	}
	ua := strings.TrimSpace(cfg.UserAgent)
	if ua == "" {
		ua = defaultUserAgent
	}

	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2:     true,
	}
	if raw := strings.TrimSpace(cfg.Proxy); raw != "" {
		pu, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("解析代理地址 %q: %w", raw, err)
		}
		switch pu.Scheme {
		case "http", "https", "socks5", "socks5h":
			tr.Proxy = http.ProxyURL(pu)
		default:
			return nil, fmt.Errorf("代理只支持 http/https/socks5，得到 %q", pu.Scheme)
		}
	}

	// 始终包装 cookieTransport：即使没有登录 Cookie，也要附加 nw=1 跳过 Offensive 警告页。
	rt := &cookieTransport{base: tr, cookie: strings.TrimSpace(cfg.Cookie)}
	checkRedirect := func(req *http.Request, via []*http.Request) error {
		// exhentai 在缺少 igneous 时会 302 到 ?poni=no 并反复跳转，直接判定为需要登录。
		if strings.Contains(req.URL.RawQuery, "poni=no") {
			return ErrAuthRequired
		}
		if len(via) >= 10 {
			return fmt.Errorf("重定向次数过多: %s", req.URL)
		}
		return nil
	}

	return &Client{
		cfg:     cfg,
		logger:  logger,
		ua:      ua,
		pacer:   NewPacer(cfg.RequestInterval, cfg.RequestJitter),
		http:    &http.Client{Transport: rt, Timeout: cfg.Timeout, CheckRedirect: checkRedirect},
		imgHTTP: &http.Client{Transport: rt, CheckRedirect: checkRedirect},
	}, nil
}

// cookieTransport 只给 E-Hentai 域附加 Cookie，防止登录态泄漏到第三方图床。
type cookieTransport struct {
	base   http.RoundTripper
	cookie string
}

func (t *cookieTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !isECookieHost(req.URL.Hostname()) {
		return t.base.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	r.Header.Set("Cookie", t.cookieHeader())
	return t.base.RoundTrip(r)
}

// suppressWarnCookie 让站点跳过「Offensive For Everyone」警告页；
// 该警告页没有缩略图容器，会让下载端误以为是站点改版。
const suppressWarnCookie = "nw=1"

func (t *cookieTransport) cookieHeader() string {
	cookie := strings.TrimSpace(t.cookie)
	if hasCookieName(cookie, "nw") {
		return cookie
	}
	if cookie == "" {
		return suppressWarnCookie
	}
	return strings.TrimSuffix(cookie, ";") + "; " + suppressWarnCookie
}

func hasCookieName(cookie, name string) bool {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if i := strings.IndexByte(part, '='); i > 0 && strings.TrimSpace(part[:i]) == name {
			return true
		}
	}
	return false
}

func isECookieHost(host string) bool {
	host = strings.ToLower(host)
	return host == "e-hentai.org" || strings.HasSuffix(host, ".e-hentai.org") ||
		host == "exhentai.org" || strings.HasSuffix(host, ".exhentai.org")
}

// do 发送请求并处理重试。重试条件：网络错误、429、5xx；403/404 立即返回分类错误。
func (c *Client) do(ctx context.Context, method, rawURL string, body []byte, headers map[string]string) (*http.Response, error) {
	attempts := c.cfg.Retries + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := range attempts {
		if err := c.pacer.Wait(ctx); err != nil {
			return nil, err
		}
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.ua)
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "*/*")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// 登录/封禁类错误重试没有意义，立即返回。
			if errors.Is(err, ErrAuthRequired) || errors.Is(err, ErrForbidden) {
				return nil, err
			}
			if attempt+1 < attempts {
				c.logger.Warn("请求失败，准备重试", "url", rawURL, "attempt", attempt+1, "err", err)
				if serr := sleepCtx(ctx, backoff(attempt)); serr != nil {
					return nil, serr
				}
				continue
			}
			return nil, fmt.Errorf("请求 %s 失败: %w", rawURL, err)
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return resp, nil
		case resp.StatusCode == http.StatusTooManyRequests:
			wait := retryAfter(resp)
			drainClose(resp.Body)
			if attempt+1 < attempts {
				if wait <= 0 {
					wait = backoff(attempt)
				}
				c.logger.Warn("被限流，等待后重试", "url", rawURL, "wait", wait)
				if serr := sleepCtx(ctx, wait); serr != nil {
					return nil, serr
				}
				continue
			}
			return nil, fmt.Errorf("%w: HTTP 429", ErrRateLimited)
		case resp.StatusCode == http.StatusForbidden:
			drainClose(resp.Body)
			return nil, fmt.Errorf("%w: HTTP 403", ErrForbidden)
		case resp.StatusCode == http.StatusNotFound:
			drainClose(resp.Body)
			return nil, fmt.Errorf("%w: HTTP 404 %s", ErrNotFound, rawURL)
		case resp.StatusCode >= 500:
			drainClose(resp.Body)
			if attempt+1 < attempts {
				c.logger.Warn("服务端错误，准备重试", "url", rawURL, "status", resp.StatusCode)
				if serr := sleepCtx(ctx, backoff(attempt)); serr != nil {
					return nil, serr
				}
				continue
			}
			return nil, fmt.Errorf("服务端错误 HTTP %d: %s", resp.StatusCode, rawURL)
		default:
			drainClose(resp.Body)
			return nil, fmt.Errorf("意外的 HTTP 状态 %d: %s", resp.StatusCode, rawURL)
		}
	}
	return nil, fmt.Errorf("请求 %s 失败", rawURL)
}

// backoff 返回 1s<<attempt 再加 0~500ms 抖动。
func backoff(attempt int) time.Duration {
	if attempt > 5 {
		attempt = 5
	}
	d := time.Duration(1<<uint(attempt)) * time.Second
	return d + time.Duration(rand.Int64N(int64(500*time.Millisecond)))
}

func retryAfter(resp *http.Response) time.Duration {
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	// 站点只使用秒数形式；HTTP-date 形式按不支持处理。
	secs, err := strconv.Atoi(raw)
	if err != nil || secs < 0 {
		return 0
	}
	if secs > 300 {
		secs = 300
	}
	return time.Duration(secs) * time.Second
}

// drainClose 丢弃少量响应体后关闭连接，让连接可复用。
func drainClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4096))
	_ = body.Close()
}

// getBytes 发起请求并读取上限内的响应体；登录页会被识别为 ErrAuthRequired。
func (c *Client) getBytes(ctx context.Context, method, rawURL string, body []byte, headers map[string]string, limit int64) ([]byte, string, error) {
	resp, err := c.do(ctx, method, rawURL, body, headers)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取 %s 响应: %w", rawURL, err)
	}
	if int64(len(data)) > limit {
		return nil, "", fmt.Errorf("%w: 响应超过 %d 字节", ErrParse, limit)
	}
	final := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	if isLoginResponse(final, data) {
		return nil, final, fmt.Errorf("%w: 站点要求登录（%s）", ErrAuthRequired, final)
	}
	if len(data) == 0 {
		return nil, final, emptyResponseError(final)
	}
	return data, final, nil
}

// emptyResponseError 区分「exhentai 缺少 igneous 时返回 200 空页」与普通异常。
func emptyResponseError(finalURL string) error {
	host := ""
	if u, err := url.Parse(finalURL); err == nil {
		host = u.Hostname()
	}
	if host == "exhentai.org" || strings.HasSuffix(host, ".exhentai.org") {
		return fmt.Errorf("%w: exhentai 返回空响应，通常是缺少 igneous Cookie", ErrAuthRequired)
	}
	return fmt.Errorf("%w: 站点返回空响应（%s）", ErrParse, finalURL)
}

func isLoginResponse(finalURL string, body []byte) bool {
	if strings.Contains(strings.ToLower(finalURL), "/login") || strings.Contains(strings.ToLower(finalURL), "bounce_login") {
		return true
	}
	return bytes.Contains(body, []byte("<title>E-Hentai.org Login")) ||
		bytes.Contains(body, []byte("<title>ExHentai.org Login"))
}

// ResolveRef 补全画廊 token：单页链接只有 pagekey，需要用 gtoken 接口换取画廊 token。
func (c *Client) ResolveRef(ctx context.Context, l Link) (GalleryRef, error) {
	if l.Token != "" {
		return GalleryRef{Site: l.Site, GID: l.GID, Token: l.Token}, nil
	}
	payload, err := json.Marshal(map[string]any{
		"method":   "gtoken",
		"pagelist": [][]any{{l.GID, l.PageToken, l.Page}},
	})
	if err != nil {
		return GalleryRef{}, err
	}
	data, _, err := c.getBytes(ctx, http.MethodPost, apiEndpoint, payload,
		map[string]string{"Content-Type": "application/json"}, maxAPIBytes)
	if err != nil {
		return GalleryRef{}, err
	}
	var resp struct {
		Error     string `json:"error"`
		TokenList []struct {
			Token string `json:"token"`
			Error string `json:"error"`
		} `json:"tokenlist"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return GalleryRef{}, fmt.Errorf("%w: gtoken 响应: %v", ErrParse, err)
	}
	if resp.Error != "" {
		return GalleryRef{}, fmt.Errorf("%w: gtoken: %s", ErrNotFound, resp.Error)
	}
	if len(resp.TokenList) == 0 || resp.TokenList[0].Token == "" {
		reason := "tokenlist 为空"
		if len(resp.TokenList) > 0 && resp.TokenList[0].Error != "" {
			reason = resp.TokenList[0].Error
		}
		return GalleryRef{}, fmt.Errorf("%w: gtoken: %s", ErrNotFound, reason)
	}
	return GalleryRef{Site: l.Site, GID: l.GID, Token: resp.TokenList[0].Token}, nil
}

// Meta 读取画廊元数据：优先 gdata API，失败时回退解析画廊页（标题与总数可用）。
func (c *Client) Meta(ctx context.Context, ref GalleryRef) (GalleryMeta, error) {
	meta, apiErr := c.metaFromAPI(ctx, ref)
	if apiErr == nil {
		return meta, nil
	}
	c.logger.Warn("gdata API 失败，回退解析画廊页", "gid", ref.GID, "err", apiErr)
	meta, htmlErr := c.metaFromHTML(ctx, ref)
	if htmlErr != nil {
		// 回退也失败时返回 API 的原始错误，便于定位真实原因（例如 token 无效）。
		return GalleryMeta{}, apiErr
	}
	return meta, nil
}

type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	*f = flexString(string(b))
	return nil
}

type apiGalleryMeta struct {
	Error     string     `json:"error"`
	Token     string     `json:"token"`
	Title     string     `json:"title"`
	TitleJPN  string     `json:"title_jpn"`
	Category  string     `json:"category"`
	Uploader  string     `json:"uploader"`
	FileCount flexString `json:"filecount"`
	FileSize  int64      `json:"filesize"`
	Posted    flexString `json:"posted"`
	Rating    flexString `json:"rating"`
	Tags      []string   `json:"tags"`
	Thumb     string     `json:"thumb"`
}

func (c *Client) metaFromAPI(ctx context.Context, ref GalleryRef) (GalleryMeta, error) {
	payload, err := json.Marshal(map[string]any{
		"method":    "gdata",
		"gidlist":   [][]any{{ref.GID, ref.Token}},
		"namespace": 1,
	})
	if err != nil {
		return GalleryMeta{}, err
	}
	data, _, err := c.getBytes(ctx, http.MethodPost, apiEndpoint, payload,
		map[string]string{"Content-Type": "application/json"}, maxAPIBytes)
	if err != nil {
		return GalleryMeta{}, err
	}
	var resp struct {
		Error     string           `json:"error"`
		GMetadata []apiGalleryMeta `json:"gmetadata"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return GalleryMeta{}, fmt.Errorf("%w: gdata 响应: %v", ErrParse, err)
	}
	if resp.Error != "" {
		return GalleryMeta{}, fmt.Errorf("%w: gdata: %s", ErrNotFound, resp.Error)
	}
	if len(resp.GMetadata) == 0 {
		return GalleryMeta{}, fmt.Errorf("%w: gdata 未返回元数据", ErrNotFound)
	}
	m := resp.GMetadata[0]
	if m.Error != "" {
		return GalleryMeta{}, fmt.Errorf("%w: gdata: %s", ErrNotFound, m.Error)
	}

	fileCount, _ := atoiComma(string(m.FileCount))
	rating, _ := strconv.ParseFloat(string(m.Rating), 64)
	var posted time.Time
	if ts, err := strconv.ParseInt(string(m.Posted), 10, 64); err == nil && ts > 0 {
		posted = time.Unix(ts, 0)
	}
	return GalleryMeta{
		GID:       ref.GID,
		Token:     ref.Token,
		Title:     m.Title,
		TitleJPN:  m.TitleJPN,
		Category:  m.Category,
		Uploader:  m.Uploader,
		FileCount: fileCount,
		FileSize:  m.FileSize,
		Posted:    posted,
		Rating:    rating,
		Tags:      m.Tags,
		Thumb:     m.Thumb,
		URL:       galleryURL(ref),
	}, nil
}

// metaFromHTML 是 API 不可用时的降级路径：只能拿到标题与页数，其余字段留空。
func (c *Client) metaFromHTML(ctx context.Context, ref GalleryRef) (GalleryMeta, error) {
	u := galleryURL(ref)
	data, _, err := c.getBytes(ctx, http.MethodGet, u, nil,
		map[string]string{"Referer": ref.Site.BaseURL() + "/"}, maxHTMLBytes)
	if err != nil {
		return GalleryMeta{}, err
	}
	gp, err := parseGalleryPage(bytes.NewReader(data))
	if err != nil {
		return GalleryMeta{}, fmt.Errorf("%w（%s）", err, u)
	}
	title, _ := parseGalleryTitle(bytes.NewReader(data))
	return GalleryMeta{
		GID:       ref.GID,
		Token:     ref.Token,
		Title:     title,
		FileCount: gp.Total,
		URL:       u,
	}, nil
}

// ListImagePages 遍历缩略图页，收集全部图片页链接。
// 停止条件来自页面上的「Showing X - Y of N images」，因为越界页码会被站点钳回最后一页。
func (c *Client) ListImagePages(ctx context.Context, ref GalleryRef, expected int) ([]ImagePage, error) {
	base := ref.Site.BaseURL()
	gallery := galleryURL(ref)

	var (
		all     []ImagePage
		seen    = make(map[int]struct{}, 64)
		total   int
		maxIter int
	)

	for p := 0; ; p++ {
		u := gallery + "?p=" + strconv.Itoa(p)
		data, _, err := c.getBytes(ctx, http.MethodGet, u, nil,
			map[string]string{"Referer": base + "/"}, maxHTMLBytes)
		if err != nil {
			return nil, err
		}
		gp, err := parseGalleryPage(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("%w（%s）", err, u)
		}

		if p == 0 {
			total = gp.Total
			if total <= 0 {
				return nil, fmt.Errorf("%w: 画廊内没有图片", ErrNotFound)
			}
			if c.cfg.MaxPages > 0 && total > c.cfg.MaxPages {
				return nil, fmt.Errorf("画廊页数 %d 超过 max_pages=%d", total, c.cfg.MaxPages)
			}
			maxIter = total/imagesPerPage + 5
			if expected > 0 && total != expected {
				c.logger.Warn("页面总数与元数据不一致，以页面为准",
					"gid", ref.GID, "page_total", total, "meta_total", expected)
			}
		}

		for _, ip := range gp.Pages {
			if _, ok := seen[ip.Page]; ok {
				continue
			}
			seen[ip.Page] = struct{}{}
			all = append(all, ImagePage{Page: ip.Page, URL: resolveURL(u, ip.URL)})
		}

		if gp.End >= gp.Total || p+1 >= maxIter {
			break
		}
	}

	sortImagePages(all)
	if len(all) < total {
		c.logger.Warn("图片链接数量少于站点声明的总数", "gid", ref.GID, "got", len(all), "total", total)
	}
	return all, nil
}

// ResolveImage 解析单页图片页，得到原图与重采样图地址。
func (c *Client) ResolveImage(ctx context.Context, p ImagePage) (ImageRef, error) {
	data, final, err := c.getBytes(ctx, http.MethodGet, p.URL, nil,
		map[string]string{"Referer": p.URL}, maxHTMLBytes)
	if err != nil {
		return ImageRef{}, err
	}
	content, err := parseImagePage(bytes.NewReader(data))
	if err != nil {
		return ImageRef{}, err
	}
	base := p.URL
	if final != "" {
		base = final
	}
	return ImageRef{
		Page:        p.Page,
		OriginalURL: resolveURL(base, content.OriginalURL),
		ImageURL:    resolveURL(base, content.ImageURL),
		Referer:     p.URL,
	}, nil
}

// resolveURL 把可能是相对路径的 href/src 补全为绝对地址。
func resolveURL(base, ref string) string {
	if ref == "" {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// sortImagePages 按页码升序排序。
func sortImagePages(pages []ImagePage) {
	slices.SortFunc(pages, func(a, b ImagePage) int { return cmp.Compare(a.Page, b.Page) })
}
