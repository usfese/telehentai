package ehentai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/time/rate"
)

// Open 依次尝试原图与重采样图，返回第一个可用的图片流。
// 调用方必须在读取完成后 Close。
//
// 未配置 Cookie 时 /fullimg/ 一定被 302 到登录页，因此回退是常态路径而不是异常路径。
func (c *Client) Open(ctx context.Context, ref ImageRef) (io.ReadCloser, ImageData, error) {
	type candidate struct {
		url          string
		usedOriginal bool
	}
	var candidates []candidate
	if c.cfg.PreferOriginal && ref.OriginalURL != "" {
		candidates = append(candidates, candidate{ref.OriginalURL, true})
	}
	if ref.ImageURL != "" && ref.ImageURL != ref.OriginalURL {
		candidates = append(candidates, candidate{ref.ImageURL, false})
	}
	if len(candidates) == 0 {
		return nil, ImageData{}, fmt.Errorf("%w: 没有可下载的图片地址", ErrParse)
	}

	var authErr, lastErr error
	for _, cand := range candidates {
		rc, data, err := c.openOne(ctx, cand.url, ref.Referer)
		if err != nil {
			if errors.Is(err, ErrAuthRequired) {
				authErr = err
			}
			lastErr = err
			continue
		}
		data.UsedFallback = !cand.usedOriginal
		return rc, data, nil
	}
	if authErr != nil {
		return nil, ImageData{}, authErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: 图片下载失败", ErrParse)
	}
	return nil, ImageData{}, lastErr
}

// openOne 下载单个候选地址；非图片响应（例如登录页 HTML）会被分类为错误。
func (c *Client) openOne(ctx context.Context, rawURL, referer string) (io.ReadCloser, ImageData, error) {
	if err := c.pacer.Wait(ctx); err != nil {
		return nil, ImageData{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, ImageData{}, err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "image/*,*/*;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := c.imgHTTP.Do(req)
	if err != nil {
		return nil, ImageData{}, fmt.Errorf("下载 %s: %w", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		head, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		final := rawURL
		if resp.Request != nil && resp.Request.URL != nil {
			final = resp.Request.URL.String()
		}
		if isLoginResponse(final, head) {
			return nil, ImageData{}, fmt.Errorf("%w: 原图需要登录（%s）", ErrAuthRequired, final)
		}
		return nil, ImageData{}, fmt.Errorf("下载 %s: HTTP %d", rawURL, resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	ext, err := extFromContentType(ct)
	if err != nil {
		_ = resp.Body.Close()
		return nil, ImageData{}, err
	}
	if resp.ContentLength > maxImageBytes {
		_ = resp.Body.Close()
		return nil, ImageData{}, fmt.Errorf("%w: 图片过大（%d 字节）", ErrParse, resp.ContentLength)
	}

	r := &imageReader{
		body:    resp.Body,
		ctx:     ctx,
		limit:   maxImageBytes,
		limiter: newBandwidthLimiter(c.cfg.BandwidthKiBPerSec),
	}
	return r, ImageData{ContentType: ct, Ext: ext, Size: resp.ContentLength}, nil
}

// newBandwidthLimiter 按 KiB/s 构造令牌桶；0 表示不限速（返回 nil）。
func newBandwidthLimiter(kibPerSec int) *rate.Limiter {
	if kibPerSec <= 0 {
		return nil
	}
	// burst 取 32 KiB：既允许小图一次性通过，又能把大图摊平到目标速率。
	return rate.NewLimiter(rate.Limit(kibPerSec*1024), 32*1024)
}

// imageReader 对底层响应体施加字节限速与大小上限。
// 限速在 Read 之后等待，保持"先产再限"的简单语义，同时仍能压住平均速率。
type imageReader struct {
	body    io.ReadCloser
	ctx     context.Context
	limiter *rate.Limiter
	limit   int64
	n       int64
}

func (r *imageReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		if r.limiter != nil {
			if werr := waitBytes(r.ctx, r.limiter, n); werr != nil {
				return n, werr
			}
		}
		r.n += int64(n)
		if r.limit > 0 && r.n > r.limit {
			return n, fmt.Errorf("%w: 图片过大", ErrParse)
		}
	}
	return n, err
}

func (r *imageReader) Close() error { return r.body.Close() }

// waitBytes 按 burst 分块等待，避免单次读取超过令牌桶容量时报错。
func waitBytes(ctx context.Context, lim *rate.Limiter, n int) error {
	burst := lim.Burst()
	if burst <= 0 {
		return lim.WaitN(ctx, n)
	}
	for n > 0 {
		chunk := n
		if chunk > burst {
			chunk = burst
		}
		if err := lim.WaitN(ctx, chunk); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
}

// extFromContentType 把常见图片 MIME 映射到文件扩展名；未知类型视为不可用。
func extFromContentType(ct string) (string, error) {
	mime := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch mime {
	case "image/webp":
		return ".webp", nil
	case "image/jpeg", "image/jpg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/gif":
		return ".gif", nil
	default:
		return "", fmt.Errorf("%w: 不支持的图片类型 %s", ErrParse, mime)
	}
}
