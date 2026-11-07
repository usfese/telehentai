package telegram

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usfese/telehentai/internal/ehentai"
	"github.com/usfese/telehentai/internal/pipeline"
)

// handle 是默认消息处理器。
func (b *Bot) handle(ctx context.Context, api *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	if !b.authorized(msg.From.ID) {
		_, _ = api.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   "未授权：请在 config.yaml 的 telegram.allowed_user_ids 中加入你的 Telegram ID（你的 ID：" + strconv.FormatInt(msg.From.ID, 10) + "）",
		})
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		b.reply(ctx, api, msg, linkHint)
		return
	}
	head := strings.Fields(text)[0]
	if head == "/start" || head == "/help" || strings.HasPrefix(head, "/start@") || strings.HasPrefix(head, "/help@") {
		b.reply(ctx, api, msg, helpText)
		return
	}

	link, err := ehentai.ParseLink(text)
	if err != nil {
		b.reply(ctx, api, msg, linkHint)
		return
	}

	sent, err := api.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          msg.Chat.ID,
		Text:            "已收到，正在解析…",
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
	})
	if err != nil {
		b.logger.Error("发送状态消息失败", "chat_id", msg.Chat.ID, "err", err)
		return
	}

	ref, err := b.resolver.ResolveRef(ctx, link)
	if err != nil {
		b.editStatus(ctx, api, msg.Chat.ID, sent.ID, errorText(err))
		return
	}

	editor := &statusEditor{
		api:       api,
		ctx:       ctx,
		chatID:    msg.Chat.ID,
		messageID: sent.ID,
		interval:  b.cfg.ProgressInterval,
		logger:    b.logger,
	}
	job := &pipeline.Job{
		ChatID:          msg.Chat.ID,
		StatusMessageID: sent.ID,
		Ref:             ref,
		Progress:        editor.progress,
	}
	job.Done = func(res pipeline.Result, err error) {
		if err != nil {
			editor.finish(errorText(err))
			return
		}
		editor.finish(resultText(res))
	}

	if !b.pipe.Submit(job) {
		editor.finish("队列已满或该画廊正在处理中，请稍后再试")
	}
}

func (b *Bot) authorized(userID int64) bool {
	if b.cfg.AllowAllUsers {
		return true
	}
	_, ok := b.cfg.AllowedUserIDs[userID]
	return ok
}

func (b *Bot) reply(ctx context.Context, api *bot.Bot, msg *models.Message, text string) {
	if _, err := api.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          msg.Chat.ID,
		Text:            text,
		ReplyParameters: &models.ReplyParameters{MessageID: msg.ID},
	}); err != nil {
		b.logger.Error("发送消息失败", "chat_id", msg.Chat.ID, "err", err)
	}
}

// editStatus 编辑状态消息；Telegram 对内容未变化的编辑返回错误，这里静默忽略。
func (b *Bot) editStatus(ctx context.Context, api *bot.Bot, chatID int64, messageID int, text string) {
	_, err := api.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      text,
	})
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		b.logger.Warn("编辑状态消息失败", "chat_id", chatID, "message_id", messageID, "err", err)
	}
}

// statusEditor 负责按节流规则更新同一条状态消息。
type statusEditor struct {
	api       *bot.Bot
	ctx       context.Context
	chatID    int64
	messageID int
	interval  time.Duration
	logger    *slog.Logger

	mu        sync.Mutex
	last      time.Time
	lastStage pipeline.Stage
}

func (e *statusEditor) progress(p pipeline.Progress) {
	e.mu.Lock()
	now := time.Now()
	if e.interval > 0 && p.Stage == e.lastStage && p.Stage != pipeline.StageDone && now.Sub(e.last) < e.interval {
		e.mu.Unlock()
		return
	}
	e.last = now
	e.lastStage = p.Stage
	e.mu.Unlock()

	e.edit(progressText(p))
}

func (e *statusEditor) finish(text string) {
	e.edit(text)
}

func (e *statusEditor) edit(text string) {
	_, err := e.api.EditMessageText(e.ctx, &bot.EditMessageTextParams{
		ChatID:    e.chatID,
		MessageID: e.messageID,
		Text:      text,
	})
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		e.logger.Warn("编辑状态消息失败", "message_id", e.messageID, "err", err)
	}
}
