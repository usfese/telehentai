package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestDefaultMatchesExample 保证内置默认值与 config.example.yaml 不会互不同步。
func TestDefaultMatchesExample(t *testing.T) {
	data, err := os.ReadFile("../../config.example.yaml")
	if err != nil {
		t.Fatalf("读取 config.example.yaml: %v", err)
	}
	var fromFile Config
	if err := yaml.Unmarshal(data, &fromFile); err != nil {
		t.Fatalf("解析 config.example.yaml: %v", err)
	}
	if got, want := Default(), &fromFile; !reflect.DeepEqual(got, want) {
		t.Fatalf("内置默认值与 config.example.yaml 不一致:\ndefault=%+v\nexample=%+v", got, want)
	}
}

func TestLoadExplicitMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")
	_, err := Load(path, true)
	if err == nil || !strings.Contains(err.Error(), "配置文件不存在") {
		t.Fatalf("期望“配置文件不存在”错误，得到 %v", err)
	}
}

func TestLoadImplicitMissingUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("期望内置默认值，得到 %+v", cfg)
	}
}

func TestLoadOverridesPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "ehentai:\n  request_interval: 2\n  request_jitter: 250ms\ntelegraph:\n  max_content_bytes: 8192\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.EHentai.RequestInterval.D(); got != 2*time.Second {
		t.Fatalf("整数应解析为秒，得到 %s", got)
	}
	if got := cfg.EHentai.RequestJitter.D(); got != 250*time.Millisecond {
		t.Fatalf("字符串时长解析错误: %s", got)
	}
	if cfg.Telegraph.MaxContentBytes != 8192 {
		t.Fatalf("max_content_bytes 未生效: %d", cfg.Telegraph.MaxContentBytes)
	}
	// 未在文件里出现的字段必须保持默认值。
	if cfg.Pipeline.Workers != 1 || cfg.Download.DataDir != "./data" {
		t.Fatalf("默认值被破坏: %+v", cfg.Pipeline)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("TELEHENTAI_TELEGRAM_TOKEN", "env-token")
	t.Setenv("TELEHENTAI_EHENTAI_COOKIE", "a=1; b=2")
	t.Setenv("TELEHENTAI_TELEGRAPH_ACCESS_TOKEN", "tok")
	t.Setenv("TELEHENTAI_TELEGRAM_ALLOWED_USER_IDS", " 42, 43 ,,")

	path := filepath.Join(t.TempDir(), "missing.yaml")
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Telegram.Token != "env-token" || cfg.EHentai.Cookie != "a=1; b=2" || cfg.Telegraph.AccessToken != "tok" {
		t.Fatalf("环境变量未覆盖: %+v", cfg)
	}
	if want := []int64{42, 43}; !reflect.DeepEqual(cfg.Telegram.AllowedUserIDs, want) {
		t.Fatalf("allowed_user_ids 解析错误: %v", cfg.Telegram.AllowedUserIDs)
	}
}

func TestLoadEnvDoesNotOverrideConfiguredValue(t *testing.T) {
	t.Setenv("TELEHENTAI_TELEGRAM_TOKEN", "env-token")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("telegram:\n  token: file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Telegram.Token != "file-token" {
		t.Fatalf("配置文件优先，得到 %q", cfg.Telegram.Token)
	}
}

func TestValidate(t *testing.T) {
	validRun := func() *Config {
		c := Default()
		c.Telegram.Token = "t"
		c.Telegram.AllowedUserIDs = []int64{1}
		c.ImageServer.PublicBaseURL = "https://img.example.com"
		return c
	}

	cases := []struct {
		name   string
		mode   Mode
		mutate func(*Config)
		want   string
	}{
		{"ok-run", ModeRun, func(*Config) {}, ""},
		{"ok-fetch-defaults", ModeFetch, func(c *Config) { c.Download.DataDir = "./data" }, ""},
		{"empty-data-dir", ModeRun, func(c *Config) { c.Download.DataDir = " " }, "data_dir"},
		{"interval-too-small", ModeRun, func(c *Config) { c.EHentai.RequestInterval = Duration(100 * time.Millisecond) }, "request_interval"},
		{"workers-zero", ModeRun, func(c *Config) { c.Pipeline.Workers = 0 }, "workers"},
		{"queue-zero", ModeRun, func(c *Config) { c.Pipeline.QueueSize = 0 }, "queue_size"},
		{"content-too-small", ModeRun, func(c *Config) { c.Telegraph.MaxContentBytes = 4095 }, "max_content_bytes"},
		{"bad-tls-mode", ModeRun, func(c *Config) { c.ImageServer.TLS.Mode = "manual" }, "tls.mode"},
		{"missing-token", ModeRun, func(c *Config) { c.Telegram.Token = "" }, "telegram.token"},
		{"missing-allowlist", ModeRun, func(c *Config) { c.Telegram.AllowedUserIDs = nil }, "allowed_user_ids"},
		{"allow-all-users", ModeRun, func(c *Config) { c.Telegram.AllowAllUsers = true; c.Telegram.AllowedUserIDs = nil }, ""},
		{"missing-public-url", ModeRun, func(c *Config) { c.ImageServer.PublicBaseURL = "" }, "public_base_url"},
		{"relative-public-url", ModeRun, func(c *Config) { c.ImageServer.PublicBaseURL = "/img" }, "绝对 URL"},
		{"http-non-loopback", ModeRun, func(c *Config) { c.ImageServer.PublicBaseURL = "http://img.example.com" }, "必须使用 https"},
		{"http-localhost-ok", ModeRun, func(c *Config) { c.ImageServer.PublicBaseURL = "http://localhost:8088" }, ""},
		{"http-127-ok", ModeRun, func(c *Config) { c.ImageServer.PublicBaseURL = "http://127.0.0.1:8088" }, ""},
		{"autocert-missing-domain", ModeRun, func(c *Config) {
			c.ImageServer.TLS.Mode = "autocert"
			c.ImageServer.TLS.Email = "a@b.c"
		}, "tls.domain"},
		{"autocert-hospital-mismatch", ModeRun, func(c *Config) {
			c.ImageServer.TLS.Mode = "autocert"
			c.ImageServer.TLS.Email = "a@b.c"
			c.ImageServer.TLS.Domain = "other.example.com"
		}, "不一致"},
		{"autocert-ok", ModeRun, func(c *Config) {
			c.ImageServer.TLS.Mode = "autocert"
			c.ImageServer.TLS.Email = "a@b.c"
			c.ImageServer.TLS.Domain = "img.example.com"
		}, ""},
		{"publish-requires-url", ModePublish, func(c *Config) { c.ImageServer.PublicBaseURL = "" }, "public_base_url"},
		{"publish-http-ok", ModePublish, func(c *Config) { c.ImageServer.PublicBaseURL = "http://127.0.0.1:18088" }, ""},
		{"serve-images-no-extra", ModeServeImages, func(c *Config) { c.ImageServer.PublicBaseURL = "" }, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validRun()
			tc.mutate(cfg)
			err := cfg.Validate(tc.mode)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("期望通过校验，得到 %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("期望错误包含 %q，得到 %v", tc.want, err)
			}
		})
	}
}

func TestDurationUnmarshalInvalid(t *testing.T) {
	var c struct {
		V Duration `yaml:"v"`
	}
	if err := yaml.Unmarshal([]byte("v: [1]\n"), &c); err == nil {
		t.Fatal("序列不应被接受为时长")
	}
}
