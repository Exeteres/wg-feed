package logx

import (
	"log/slog"
	"testing"
)

func TestSlogLevelFromEnv_DefaultInfo(t *testing.T) {
	t.Setenv(envLogLevel, "")
	if got := SlogLevelFromEnv(); got != slog.LevelInfo {
		t.Fatalf("expected info, got %v", got)
	}
}

func TestSlogLevelFromEnv_ValidLevel(t *testing.T) {
	t.Setenv(envLogLevel, "debug")
	if got := SlogLevelFromEnv(); got != slog.LevelDebug {
		t.Fatalf("expected debug, got %v", got)
	}
}

func TestSlogLevelFromEnv_CaseInsensitive(t *testing.T) {
	t.Setenv(envLogLevel, "WARN")
	if got := SlogLevelFromEnv(); got != slog.LevelWarn {
		t.Fatalf("expected warn, got %v", got)
	}
}

func TestSlogLevelFromEnv_InvalidFallsBackToInfo(t *testing.T) {
	t.Setenv(envLogLevel, "nope")
	if got := SlogLevelFromEnv(); got != slog.LevelInfo {
		t.Fatalf("expected info fallback, got %v", got)
	}
}
