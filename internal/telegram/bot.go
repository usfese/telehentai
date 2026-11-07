// Package telegram 实现 Telegram 机器人：授权、链接解析、进度与结果回执。
package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/pipeline"
)

// Resolver 把单页链接补全为画廊引用；*ehentai.Client 满足该接口。
type Resolver interface {
	ResolveRef(context.Context, ehentai.Link) (ehentai.GalleryRef, error)
}

// Config 是机器人配置。
type Config struct {
	Token            string
	AllowedUserIDs   map[int64]struct{}
	AllowAllUsers    bool
	ProgressInterval time.Duration
}

// Bot 把 Telegram 更新接到流水线。
type Bot struct {
	api      *bot.Bot
	cfg      Config
	pipe     *pipeline.Pipeline
	resolver Resolver
	logger   *slog.Logger
}

// New 构造机器人；token 为空时报错。
func New(cfg Config, pipe *pipeline.Pipeline, resolver Resolver, logger *slog.Logger) (*Bot, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("telegram: token 不能为空")
	}
	if logger == nil {
		logger = slog.Default()
	}
	b := &Bot{cfg: cfg, pipe: pipe, resolver: resolver, logger: logger}

	api, err := bot.New(cfg.Token,
		bot.WithDefaultHandler(func(ctx context.Context, api *bot.Bot, update *models.Update) {
			b.handle(ctx, api, update)
		}),
		bot.WithErrorsHandler(func(err error) {
			b.logger.Error("Telegram 请求错误", "err", err)
		}),
	)
	if err != nil {
		return nil, err
	}
	b.api = api
	return b, nil
}

// Run 阻塞地开始长轮询，直到 ctx 取消。
func (b *Bot) Run(ctx context.Context) error {
	b.logger.Info("Telegram 机器人启动（长轮询）")
	b.api.Start(ctx)
	return nil
}
