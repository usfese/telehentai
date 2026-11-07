// Package logx 构造全项目统一的 slog logger。
package logx

import (
	"log/slog"
	"os"
	"strings"
)

// New 按级别（debug/info/warn/error）与格式（text/json）返回 logger。
// 未知值回落到 info/text，避免配置笔误导致进程退出。
func New(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	if strings.EqualFold(format, "json") {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
