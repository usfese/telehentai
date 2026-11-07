// Package config 负责加载、环境变量覆盖与校验 telehentai 的 YAML 配置。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration 是可在 YAML 中写成 "1s" 字符串或整数秒的时长。
type Duration time.Duration

// D 返回标准库时长，便于与 time 包互操作。
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalYAML 同时接受 `"1s"` 这类字符串与 `1`（秒）这类整数。
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	switch node.Tag {
	case "!!null":
		// 空值保持默认，方便用户注释掉某个字段而不是写 0。
		return nil
	case "!!int":
		n, err := strconv.ParseInt(node.Value, 10, 64)
		if err != nil {
			return fmt.Errorf("解析秒数 %q: %w", node.Value, err)
		}
		*d = Duration(time.Duration(n) * time.Second)
		return nil
	case "!!str":
		v, err := time.ParseDuration(node.Value)
		if err != nil {
			return fmt.Errorf("解析时长 %q: %w", node.Value, err)
		}
		*d = Duration(v)
		return nil
	default:
		return fmt.Errorf("时长只接受字符串或整数秒，得到 %s", node.Tag)
	}
}

// Config 是完整配置树；每个字段都显式声明 yaml tag，保证 key 与文档一致。
type Config struct {
	Telegram    TelegramConfig    `yaml:"telegram"`
	EHentai     EHentaiConfig     `yaml:"ehentai"`
	Download    DownloadConfig    `yaml:"download"`
	Telegraph   TelegraphConfig   `yaml:"telegraph"`
	ImageServer ImageServerConfig `yaml:"image_server"`
	Pipeline    PipelineConfig    `yaml:"pipeline"`
	Log         LogConfig         `yaml:"log"`
}

// TelegramConfig 描述 Telegram 机器人凭据与访问控制。
type TelegramConfig struct {
	Token          string  `yaml:"token"`
	AllowedUserIDs []int64 `yaml:"allowed_user_ids"`
	AllowAllUsers  bool    `yaml:"allow_all_users"`
}

// EHentaiConfig 描述 E-Hentai 抓取行为：鉴权、限速、超时与重试。
type EHentaiConfig struct {
	Cookie             string   `yaml:"cookie"`
	UserAgent          string   `yaml:"user_agent"`
	Proxy              string   `yaml:"proxy"`
	RequestInterval    Duration `yaml:"request_interval"`
	RequestJitter      Duration `yaml:"request_jitter"`
	BandwidthKiBPerSec int      `yaml:"bandwidth_kib_per_sec"`
	Timeout            Duration `yaml:"timeout"`
	Retries            int      `yaml:"retries"`
	PreferOriginal     bool     `yaml:"prefer_original"`
	MaxPages           int      `yaml:"max_pages"`
}

// DownloadConfig 描述本地缓存目录。
type DownloadConfig struct {
	DataDir string `yaml:"data_dir"`
}

// TelegraphConfig 描述 telegra.ph 发布参数。
type TelegraphConfig struct {
	AccessToken     string   `yaml:"access_token"`
	AuthorName      string   `yaml:"author_name"`
	AuthorURL       string   `yaml:"author_url"`
	MaxContentBytes int      `yaml:"max_content_bytes"`
	RequestInterval Duration `yaml:"request_interval"`
}

// ImageServerConfig 描述自建图床的监听地址与对外地址。
type ImageServerConfig struct {
	Listen        string    `yaml:"listen"`
	PublicBaseURL string    `yaml:"public_base_url"`
	TLS           TLSConfig `yaml:"tls"`
}

// TLSConfig 描述图床的 TLS 暴露方式。
type TLSConfig struct {
	Mode       string `yaml:"mode"`
	Domain     string `yaml:"domain"`
	Email      string `yaml:"email"`
	CacheDir   string `yaml:"cache_dir"`
	HTTPListen string `yaml:"http_listen"`
}

// PipelineConfig 描述任务队列与进度上报。
type PipelineConfig struct {
	Workers          int      `yaml:"workers"`
	QueueSize        int      `yaml:"queue_size"`
	ProgressInterval Duration `yaml:"progress_interval"`
}

// LogConfig 描述日志级别与格式。
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Mode 表示进程启动模式；不同模式校验的必填项不同。
type Mode int

const (
	// ModeRun 是完整机器人模式：Telegram + 下载 + 图床 + Telegraph。
	ModeRun Mode = iota
	// ModeFetch 只下载画廊到本地。
	ModeFetch
	// ModePublish 只把本地图片发布为 Telegraph 页面。
	ModePublish
	// ModeServeImages 只启动图床。
	ModeServeImages
)

// String 返回模式名，用于日志与错误信息。
func (m Mode) String() string {
	switch m {
	case ModeRun:
		return "run"
	case ModeFetch:
		return "fetch"
	case ModePublish:
		return "publish"
	case ModeServeImages:
		return "serve-images"
	default:
		return "unknown"
	}
}

// Default 返回与 config.example.yaml 完全一致的内置默认配置。
func Default() *Config {
	return &Config{
		Telegram: TelegramConfig{
			Token:          "",
			AllowedUserIDs: []int64{},
			AllowAllUsers:  false,
		},
		EHentai: EHentaiConfig{
			Cookie:             "",
			UserAgent:          "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			Proxy:              "",
			RequestInterval:    Duration(time.Second),
			RequestJitter:      Duration(300 * time.Millisecond),
			BandwidthKiBPerSec: 512,
			Timeout:            Duration(60 * time.Second),
			Retries:            3,
			PreferOriginal:     true,
			MaxPages:           5000,
		},
		Download: DownloadConfig{DataDir: "./data"},
		Telegraph: TelegraphConfig{
			AccessToken:     "",
			AuthorName:      "telehentai",
			AuthorURL:       "",
			MaxContentBytes: 49152,
			RequestInterval: Duration(time.Second),
		},
		ImageServer: ImageServerConfig{
			Listen:        ":8088",
			PublicBaseURL: "",
			TLS: TLSConfig{
				Mode:       "off",
				Domain:     "",
				Email:      "",
				CacheDir:   "./data/autocert",
				HTTPListen: ":80",
			},
		},
		Pipeline: PipelineConfig{
			Workers:          1,
			QueueSize:        8,
			ProgressInterval: Duration(5 * time.Second),
		},
		Log: LogConfig{Level: "info", Format: "text"},
	}
}

// Load 读取配置文件。explicit 为 true 时文件必须存在；否则缺省文件按内置默认值运行，
// 便于只跑 fetch/publish 的调试场景。文件里的字段覆盖默认值，环境变量再覆盖文件。
func Load(path string, explicit bool) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("读取配置文件 %s: %w", path, err)
		}
		if explicit {
			return nil, fmt.Errorf("配置文件不存在: %s", path)
		}
		slog.Info("未找到配置文件，使用内置默认值", "path", path)
		if err := applyEnv(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s: %w", path, err)
	}
	if err := applyEnv(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyEnv 用环境变量补齐为空的敏感字段；非空配置优先，避免环境变量意外覆盖。
func applyEnv(c *Config) error {
	if c.Telegram.Token == "" {
		c.Telegram.Token = os.Getenv("TELEHENTAI_TELEGRAM_TOKEN")
	}
	if c.EHentai.Cookie == "" {
		c.EHentai.Cookie = os.Getenv("TELEHENTAI_EHENTAI_COOKIE")
	}
	if c.Telegraph.AccessToken == "" {
		c.Telegraph.AccessToken = os.Getenv("TELEHENTAI_TELEGRAPH_ACCESS_TOKEN")
	}
	if len(c.Telegram.AllowedUserIDs) == 0 {
		if raw := strings.TrimSpace(os.Getenv("TELEHENTAI_TELEGRAM_ALLOWED_USER_IDS")); raw != "" {
			ids, err := parseUserIDs(raw)
			if err != nil {
				return fmt.Errorf("解析 TELEHENTAI_TELEGRAM_ALLOWED_USER_IDS: %w", err)
			}
			c.Telegram.AllowedUserIDs = ids
		}
	}
	return nil
}

func parseUserIDs(raw string) ([]int64, error) {
	fields := strings.Split(raw, ",")
	ids := make([]int64, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		id, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q 不是整数: %w", f, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Validate 按启动模式校验配置，返回的错误信息可直接展示给部署者。
func (c *Config) Validate(mode Mode) error {
	if strings.TrimSpace(c.Download.DataDir) == "" {
		return errors.New("download.data_dir 不能为空")
	}
	if c.EHentai.RequestInterval.D() < 200*time.Millisecond {
		return fmt.Errorf("ehentai.request_interval 必须 >= 200ms，当前 %s", c.EHentai.RequestInterval.D())
	}
	if c.Pipeline.Workers < 1 {
		return fmt.Errorf("pipeline.workers 必须 >= 1，当前 %d", c.Pipeline.Workers)
	}
	if c.Pipeline.QueueSize < 1 {
		return fmt.Errorf("pipeline.queue_size 必须 >= 1，当前 %d", c.Pipeline.QueueSize)
	}
	if c.Telegraph.MaxContentBytes < 4096 {
		return fmt.Errorf("telegraph.max_content_bytes 必须 >= 4096，当前 %d", c.Telegraph.MaxContentBytes)
	}
	switch c.ImageServer.TLS.Mode {
	case "", "off", "autocert":
	default:
		return fmt.Errorf("image_server.tls.mode 只能是 off 或 autocert，当前 %q", c.ImageServer.TLS.Mode)
	}

	switch mode {
	case ModeRun:
		if strings.TrimSpace(c.Telegram.Token) == "" {
			return errors.New("telegram.token 不能为空")
		}
		if !c.Telegram.AllowAllUsers && len(c.Telegram.AllowedUserIDs) == 0 {
			return errors.New("telegram.allowed_user_ids 不能为空（或设置 telegram.allow_all_users: true）")
		}
		return c.validatePublicBaseURL(true)
	case ModePublish:
		return c.validatePublicBaseURL(false)
	case ModeFetch, ModeServeImages:
		return nil
	default:
		return fmt.Errorf("未知启动模式: %d", mode)
	}
}

// validatePublicBaseURL 校验图床对外地址。requireHTTPS 只在 run 模式开启：
// 发布时允许 localhost 的 http 地址，便于本地冒烟。
func (c *Config) validatePublicBaseURL(requireHTTPS bool) error {
	raw := strings.TrimSpace(c.ImageServer.PublicBaseURL)
	if raw == "" {
		return errors.New("image_server.public_base_url 不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("image_server.public_base_url 不是合法的 URL: %q: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("image_server.public_base_url 必须是绝对 URL（含 scheme 与 host）: %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("image_server.public_base_url 只支持 http/https: %q", raw)
	}
	if requireHTTPS && u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("public_base_url 必须使用 https：telegra.ph 页面会拦截 http 图片（当前 %q）", raw)
	}
	if c.ImageServer.TLS.Mode == "autocert" {
		if strings.TrimSpace(c.ImageServer.TLS.Domain) == "" {
			return errors.New("tls.mode=autocert 时 image_server.tls.domain 不能为空")
		}
		if strings.TrimSpace(c.ImageServer.TLS.Email) == "" {
			return errors.New("tls.mode=autocert 时 image_server.tls.email 不能为空")
		}
		if u.Hostname() != c.ImageServer.TLS.Domain {
			return fmt.Errorf("image_server.public_base_url 的 host（%s）与 image_server.tls.domain（%s）不一致，autocert 需要二者相同",
				u.Hostname(), c.ImageServer.TLS.Domain)
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
