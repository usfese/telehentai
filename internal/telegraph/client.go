// Package telegraph 实现 telegra.ph 的页面创建/编辑与内容节点构建。
//
// 约束：Telegraph 于 2024-09 关闭了图片上传接口，因此这里只发布指向自建图床的外链图片；
// 单页内容 JSON 有体积上限，超限时由上层按预算回退重试。
package telegraph

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Node 是 Telegraph 内容节点（DOM 风格的最小结构）。
type Node struct {
	Tag      string            `json:"tag"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	Children []any             `json:"children,omitempty"`
}

// PageInput 是一次创建/编辑页面的入参。
type PageInput struct {
	Title      string
	Content    []Node
	AuthorName string
	AuthorURL  string
}

// Page 是创建/编辑成功后返回的页面信息。
type Page struct {
	Path  string
	URL   string
	Title string
}

// Config 是 Telegraph 客户端配置。
type Config struct {
	AccessToken     string
	AuthorName      string
	AuthorURL       string
	RequestInterval time.Duration
}

// TokenStore 负责持久化自动创建的 access_token。
type TokenStore interface {
	Load() (string, error)
	Save(string) error
}

// ErrContentTooBig 表示页面内容 JSON 超过 Telegraph 限制，调用方应减小预算后重试。
var ErrContentTooBig = errors.New("telegraph: content too big")

const defaultAPIBase = "https://api.telegra.ph"

var floodWaitRe = regexp.MustCompile(`^FLOOD_WAIT_(\d+)$`)

// Client 是 telegra.ph API 客户端；并发安全。
type Client struct {
	apiBase    string
	http       *http.Client
	authorName string
	authorURL  string
	interval   time.Duration
	tokens     TokenStore
	logger     *slog.Logger
	sleep      func(context.Context, time.Duration) error

	mu       sync.Mutex
	token    string // 内存缓存，避免每页都请求 createAccount
	cfgToken string
}

// New 构造客户端。tokens 可为 nil（此时只能依赖配置中的 token）。
func New(cfg Config, tokens TokenStore, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	interval := cfg.RequestInterval
	if interval < 0 {
		interval = 0
	}
	return &Client{
		apiBase:    defaultAPIBase,
		http:       &http.Client{Timeout: 30 * time.Second},
		authorName: cfg.AuthorName,
		authorURL:  cfg.AuthorURL,
		interval:   interval,
		tokens:     tokens,
		logger:     logger,
		sleep:      sleepCtx,
		cfgToken:   strings.TrimSpace(cfg.AccessToken),
	}
}

// EnsureToken 返回可用的 access_token：优先配置，其次持久化，最后自动创建账号。
func (c *Client) EnsureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" {
		return c.token, nil
	}
	if c.cfgToken != "" {
		c.token = c.cfgToken
		return c.token, nil
	}
	if c.tokens != nil {
		if tok, err := c.tokens.Load(); err != nil {
			c.logger.Warn("读取已保存的 Telegraph token 失败，将重新创建账号", "err", err)
		} else if tok != "" {
			c.token = tok
			return c.token, nil
		}
	}
	tok, err := c.createAccount(ctx)
	if err != nil {
		return "", err
	}
	c.token = tok
	if c.tokens != nil {
		if err := c.tokens.Save(tok); err != nil {
			c.logger.Warn("保存 Telegraph token 失败", "err", err)
		}
	}
	c.logger.Info("已创建 Telegraph 账号", "author", c.authorName)
	return tok, nil
}

// createAccount 注册一个匿名 telegra.ph 账号并返回 access_token。
func (c *Client) createAccount(ctx context.Context) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成 short_name: %w", err)
	}
	q := url.Values{}
	q.Set("short_name", "telehentai-"+hex.EncodeToString(buf))
	if c.authorName != "" {
		q.Set("author_name", c.authorName)
	}
	if err := c.rateWait(ctx); err != nil {
		return "", err
	}
	endpoint := c.apiBase + "/createAccount?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("创建 Telegraph 账号: %w", err)
	}
	defer resp.Body.Close()
	apiResp, err := decodeResponse(resp.Body)
	if err != nil {
		return "", err
	}
	if !apiResp.OK || apiResp.Result.AccessToken == "" {
		return "", fmt.Errorf("创建 Telegraph 账号失败: %s", apiResp.Error)
	}
	return apiResp.Result.AccessToken, nil
}

// CreatePage 新建页面。
func (c *Client) CreatePage(ctx context.Context, in PageInput) (Page, error) {
	return c.submit(ctx, "createPage", "", in)
}

// EditPage 编辑已有页面（按 path 定位）。
func (c *Client) EditPage(ctx context.Context, path string, in PageInput) (Page, error) {
	if path == "" {
		return Page{}, errors.New("telegraph: editPage 缺少 path")
	}
	return c.submit(ctx, "editPage", path, in)
}

// submit 提交页面并处理三类可恢复错误：内容过大、token 失效、洪水保护。
// token 失效与洪水保护各只重试一次，避免无限循环。
func (c *Client) submit(ctx context.Context, method, path string, in PageInput) (Page, error) {
	content, err := json.Marshal(in.Content)
	if err != nil {
		return Page{}, fmt.Errorf("序列化内容: %w", err)
	}
	authorName := in.AuthorName
	if authorName == "" {
		authorName = c.authorName
	}
	authorURL := in.AuthorURL
	if authorURL == "" {
		authorURL = c.authorURL
	}

	for attempt := range 2 {
		token, err := c.EnsureToken(ctx)
		if err != nil {
			return Page{}, err
		}
		form := url.Values{}
		form.Set("access_token", token)
		form.Set("title", in.Title)
		form.Set("content", string(content))
		form.Set("return_content", "false")
		if authorName != "" {
			form.Set("author_name", authorName)
		}
		if authorURL != "" {
			form.Set("author_url", authorURL)
		}
		if path != "" {
			form.Set("path", path)
		}

		if err := c.rateWait(ctx); err != nil {
			return Page{}, err
		}
		apiResp, err := c.postForm(ctx, method, form)
		if err != nil {
			return Page{}, err
		}
		if apiResp.OK {
			p := Page{
				Path:  apiResp.Result.Path,
				URL:   "https://telegra.ph/" + apiResp.Result.Path,
				Title: apiResp.Result.Title,
			}
			if p.Title == "" {
				p.Title = in.Title
			}
			return p, nil
		}

		switch {
		case apiResp.Error == "CONTENT_TOO_BIG":
			return Page{}, ErrContentTooBig
		case apiResp.Error == "ACCESS_TOKEN_INVALID":
			if attempt == 0 {
				c.invalidateToken()
				continue
			}
			return Page{}, fmt.Errorf("telegraph %s: %s", method, apiResp.Error)
		case floodWaitRe.MatchString(apiResp.Error):
			if attempt == 0 {
				secs, _ := strconv.Atoi(floodWaitRe.FindStringSubmatch(apiResp.Error)[1])
				c.logger.Warn("Telegraph 洪水保护，等待后重试", "wait_seconds", secs)
				if err := c.sleep(ctx, time.Duration(secs)*time.Second); err != nil {
					return Page{}, err
				}
				continue
			}
			return Page{}, fmt.Errorf("telegraph %s: %s", method, apiResp.Error)
		default:
			return Page{}, fmt.Errorf("telegraph %s: %s", method, apiResp.Error)
		}
	}
	return Page{}, fmt.Errorf("telegraph %s: 重试后仍失败", method)
}

func (c *Client) invalidateToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 配置里的 token 无法通过清缓存"刷新"，但上层只会重试一次，不会死循环。
	c.token = ""
}

// rateWait 在每次请求前等待固定间隔，降低触发洪水保护的概率。
func (c *Client) rateWait(ctx context.Context) error {
	if c.interval <= 0 {
		return ctx.Err()
	}
	return sleepCtx(ctx, c.interval)
}

type apiResult struct {
	Path        string `json:"path"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	AccessToken string `json:"access_token"`
}

type apiResponse struct {
	OK     bool      `json:"ok"`
	Result apiResult `json:"result"`
	Error  string    `json:"error"`
}

func (c *Client) postForm(ctx context.Context, method string, form url.Values) (*apiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegraph %s: %w", method, err)
	}
	defer resp.Body.Close()
	apiResp, err := decodeResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("telegraph %s: %w", method, err)
	}
	return apiResp, nil
}

func decodeResponse(r io.Reader) (*apiResponse, error) {
	data, err := io.ReadAll(io.LimitReader(r, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应: %w", err)
	}
	var resp apiResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析响应: %w", err)
	}
	return &resp, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
