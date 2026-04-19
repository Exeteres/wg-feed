package logx

import (
	"log/slog"
	"os"
	"strings"
)

const envLogLevel = "LOG_LEVEL"

func NewStdoutLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: SlogLevelFromEnv()}))
}

// SlogLevelFromEnv returns the slog level from LOG_LEVEL.
// Supported values include debug, info, warn, and error.
// Invalid or empty values fall back to info.
func SlogLevelFromEnv() slog.Level {
	raw := strings.TrimSpace(os.Getenv(envLogLevel))
	if raw == "" {
		return slog.LevelInfo
	}

	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.ToLower(raw))); err != nil {
		return slog.LevelInfo
	}
	return lvl
}
