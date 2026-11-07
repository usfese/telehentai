// Package app 负责把各组件装配成完整服务并管理生命周期。
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/usfese/telehentai/internal/config"
	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/imgsrv"
	"github.com/usfese/telehentai/internal/pipeline"
	"github.com/usfese/telehentai/internal/store"
	"github.com/usfese/telehentai/internal/telegram"
	"github.com/usfese/telehentai/internal/telegraph"
)

// Run 装配并运行图床与 Telegram 机器人，任一组件返回错误即整体退出。
func Run(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	st, err := store.New(cfg.Download.DataDir)
	if err != nil {
		return err
	}
	src, err := ehentai.New(EhentaiConfig(cfg), logger)
	if err != nil {
		return err
	}
	pub := telegraph.New(TelegraphConfig(cfg), tokenStore{st: st}, logger)
	pipe := pipeline.New(PipelineConfig(cfg), src, pub, st, logger)
	img, err := imgsrv.New(ImageServerConfig(cfg), st, logger)
	if err != nil {
		return err
	}
	tg, err := telegram.New(telegram.Config{
		Token:            cfg.Telegram.Token,
		AllowedUserIDs:   userIDSet(cfg.Telegram.AllowedUserIDs),
		AllowAllUsers:    cfg.Telegram.AllowAllUsers,
		ProgressInterval: cfg.Pipeline.ProgressInterval.D(),
	}, pipe, src, logger)
	if err != nil {
		return err
	}

	pipe.Start(ctx)
	go selfCheck(ctx, cfg.ImageServer.PublicBaseURL, logger)

	errCh := make(chan error, 1)
	report := func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}
	go func() {
		if err := img.Run(ctx); err != nil {
			report(fmt.Errorf("图床: %w", err))
		}
	}()
	go func() {
		if err := tg.Run(ctx); err != nil {
			report(fmt.Errorf("Telegram: %w", err))
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("收到退出信号，正在关闭")
		return nil
	case err := <-errCh:
		cancel()
		return err
	}
}

// selfCheck 探测图床公网地址；失败只告警，不阻塞启动。
func selfCheck(ctx context.Context, baseURL string, logger *slog.Logger) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(1500 * time.Millisecond):
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/healthz")
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			logger.Info("图床自检通过", "base_url", baseURL)
			return
		}
		err = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	logger.Warn("图床公网地址不可达，Telegraph 页面中的图片可能无法显示", "base_url", baseURL, "err", err)
}

// tokenStore 让 *store.Store 满足 telegraph.TokenStore。
type tokenStore struct{ st *store.Store }

func (t tokenStore) Load() (string, error) { return t.st.TelegraphToken() }

func (t tokenStore) Save(s string) error { return t.st.SaveTelegraphToken(s) }

func userIDSet(ids []int64) map[int64]struct{} {
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

func EhentaiConfig(cfg *config.Config) ehentai.Config {
	return ehentai.Config{
		Cookie:             cfg.EHentai.Cookie,
		UserAgent:          cfg.EHentai.UserAgent,
		Proxy:              cfg.EHentai.Proxy,
		RequestInterval:    cfg.EHentai.RequestInterval.D(),
		RequestJitter:      cfg.EHentai.RequestJitter.D(),
		BandwidthKiBPerSec: cfg.EHentai.BandwidthKiBPerSec,
		Timeout:            cfg.EHentai.Timeout.D(),
		Retries:            cfg.EHentai.Retries,
		PreferOriginal:     cfg.EHentai.PreferOriginal,
		MaxPages:           cfg.EHentai.MaxPages,
	}
}

func PipelineConfig(cfg *config.Config) pipeline.Config {
	return pipeline.Config{
		Workers:             cfg.Pipeline.Workers,
		QueueSize:           cfg.Pipeline.QueueSize,
		ProgressInterval:    cfg.Pipeline.ProgressInterval.D(),
		MaxContentBytes:     cfg.Telegraph.MaxContentBytes,
		DownloadConcurrency: cfg.Pipeline.DownloadConcurrency,
		MaxImages:           cfg.Pipeline.MaxImages,
		PublicBaseURL:       strings.TrimRight(cfg.ImageServer.PublicBaseURL, "/"),
		AuthorName:          cfg.Telegraph.AuthorName,
		AuthorURL:           cfg.Telegraph.AuthorURL,
	}
}

func TelegraphConfig(cfg *config.Config) telegraph.Config {
	return telegraph.Config{
		AccessToken:     cfg.Telegraph.AccessToken,
		AuthorName:      cfg.Telegraph.AuthorName,
		AuthorURL:       cfg.Telegraph.AuthorURL,
		RequestInterval: cfg.Telegraph.RequestInterval.D(),
	}
}

func ImageServerConfig(cfg *config.Config) imgsrv.Config {
	return imgsrv.Config{
		Listen:        cfg.ImageServer.Listen,
		PublicBaseURL: cfg.ImageServer.PublicBaseURL,
		TLS: imgsrv.TLSConfig{
			Mode:       cfg.ImageServer.TLS.Mode,
			Domain:     cfg.ImageServer.TLS.Domain,
			Email:      cfg.ImageServer.TLS.Email,
			CacheDir:   cfg.ImageServer.TLS.CacheDir,
			HTTPListen: cfg.ImageServer.TLS.HTTPListen,
		},
	}
}
