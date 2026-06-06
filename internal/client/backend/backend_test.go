package backend

import (
	"io"
	"log/slog"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/config"
)

func TestNewForType_KnownBackends(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	types := []config.BackendType{
		config.BackendWGQuick,
		config.BackendNetworkManager,
		config.BackendNetNS,
		config.BackendWindows,
		config.BackendWindowsManager,
	}

	for _, backendType := range types {
		backendType := backendType
		t.Run(string(backendType), func(t *testing.T) {
			t.Parallel()

			b, err := NewForType(backendType, logger)
			if err != nil {
				t.Fatalf("NewForType(%q) error: %v", backendType, err)
			}
			if b == nil {
				t.Fatalf("NewForType(%q) returned nil backend", backendType)
			}
		})
	}
}

func TestNewForType_UnknownBackend(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := NewForType(config.BackendType("unknown"), logger); err == nil {
		t.Fatalf("expected error for unknown backend type")
	}
}
