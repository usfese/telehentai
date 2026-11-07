package telegram

import (
	"context"
	"log/slog"
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
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	b.logger.Debug("收到消息", "chat_id", msg.Chat.ID, "chat_type", string(msg.Chat.Type), "from", msg.From.ID, "text_len", len(text))

	// 只响应命令与 E-Hentai 链接，其余消息（聊天、图片、贴纸等）一律静默忽略。
	head := strings.Fields(text)[0]
	if strings.HasPrefix(head, "/") {
		if isHelpCommand(head) {
			b.reply(ctx, api, msg, helpText)
		}
		return
	}

	link, ok := extractLink(text)
	if !ok {
		b.logger.Debug("忽略非链接消息", "chat_id", msg.Chat.ID, "preview", truncate(text, 80))
		return
	}

	if !b.authorized(msg.From.ID) {
		// 私聊里告知无权限；群里静默，避免每个人贴链接都刷一条拒绝消息。
		b.logger.Warn("未授权用户尝试使用", "user_id", msg.From.ID, "username", msg.From.Username, "chat_id", msg.Chat.ID)
		if msg.Chat.Type == models.ChatTypePrivate {
			b.reply(ctx, api, msg, noAccessText)
		}
		return
	}

	sent, err := api.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   "已收到，正在解析…",
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
			editor.finish(errorText(err), "", "")
			return
		}
		if res.Rejected {
			editor.finish(rejectedText(res), models.ParseModeHTML, "")
			if res.CoverURL != "" {
				b.sendPhoto(msg.Chat.ID, res.CoverURL)
			}
			return
		}
		// 消息里第一个链接是源画廊（Telegram 无法预览），显式指定预览 Telegraph 页面。
		editor.finish(resultText(res), models.ParseModeHTML, firstPageURL(res))
	}

	if !b.pipe.Submit(job) {
		editor.finish(busyText, "", "")
	}
}

// extractLink 从纯链接或带说明文字的消息里提取画廊链接。
func extractLink(text string) (ehentai.Link, bool) {
	if l, err := ehentai.ParseLink(strings.TrimSpace(text)); err == nil {
		return l, true
	}
	for _, field := range strings.Fields(text) {
		candidate := strings.Trim(field, "()[]{}<>\"'`,，。；;!！?？")
		if l, err := ehentai.ParseLink(candidate); err == nil {
			return l, true
		}
	}
	return ehentai.Link{}, false
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// isHelpCommand 匹配 /start、/help，以及群里的 /start@botname 形式。
func isHelpCommand(head string) bool {
	if head == "/start" || head == "/help" {
		return true
	}
	name, _, ok := strings.Cut(head, "@")
	return ok && (name == "/start" || name == "/help")
}

func (b *Bot) authorized(userID int64) bool {
	if b.cfg.AllowAllUsers {
		return true
	}
	_, ok := b.cfg.AllowedUserIDs[userID]
	return ok
}

// sendPhoto 把封面图（图床 URL）发给用户；失败只记日志。
func (b *Bot) sendPhoto(chatID int64, photoURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := b.api.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID: chatID,
		Photo:  &models.InputFileString{Data: photoURL},
	}); err != nil {
		b.logger.Warn("发送封面失败", "chat_id", chatID, "err", err)
	}
}

func (b *Bot) reply(ctx context.Context, api *bot.Bot, msg *models.Message, text string) {
	if _, err := api.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   text,
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

	e.edit(progressText(p), "", "")
}

func (e *statusEditor) finish(text string, parseMode models.ParseMode, previewURL string) {
	e.edit(text, parseMode, previewURL)
}

func firstPageURL(res pipeline.Result) string {
	if len(res.Pages) > 0 {
		return res.Pages[0].URL
	}
	if len(res.URLs) > 0 {
		return res.URLs[0]
	}
	return ""
}

func (e *statusEditor) edit(text string, parseMode models.ParseMode, previewURL string) {
	// 结果回调发生在 worker 中，可能晚于本次更新的 ctx；这里用独立超时上下文。
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	params := &bot.EditMessageTextParams{
		ChatID:    e.chatID,
		MessageID: e.messageID,
		Text:      text,
		ParseMode: parseMode,
	}
	if previewURL != "" {
		// 显式指定预览 URL 并优先大图；否则 Telegram 只会取消息里的第一个链接（源画廊不可预览）。
		params.LinkPreviewOptions = &models.LinkPreviewOptions{
			URL:              &previewURL,
			PreferLargeMedia: bot.True(),
		}
	}
	_, err := e.api.EditMessageText(ctx, params)
	if err != nil && !strings.Contains(err.Error(), "message is not modified") {
		e.logger.Warn("编辑状态消息失败", "message_id", e.messageID, "err", err)
	}
}
