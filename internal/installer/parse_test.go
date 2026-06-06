package installer

import (
	"testing"

	"github.com/exeteres/wg-feed/internal/client/config"
)

func TestParseURLs(t *testing.T) {
	urls, err := ParseURLs("https://a.example/sub https://b.example/sub")
	if err != nil {
		t.Fatalf("ParseURLs error: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 urls got %d", len(urls))
	}
}

func TestParseBackendsInput(t *testing.T) {
	bs, err := ParseBackendsInput("wg-quick,netns")
	if err != nil {
		t.Fatalf("ParseBackendsInput error: %v", err)
	}
	if len(bs) != 2 || bs[0] != config.BackendWGQuick || bs[1] != config.BackendNetNS {
		t.Fatalf("unexpected backends: %#v", bs)
	}
}

func TestParseBackendsInput_WindowsManager(t *testing.T) {
	bs, err := ParseBackendsInput("windows-manager,windows")
	if err != nil {
		t.Fatalf("ParseBackendsInput error: %v", err)
	}
	if len(bs) != 2 || bs[0] != config.BackendWindowsManager || bs[1] != config.BackendWindows {
		t.Fatalf("unexpected backends: %#v", bs)
	}
}

func TestParseTunnelInput_All(t *testing.T) {
	choice, err := ParseTunnelInput("all", []string{"t1", "t2"})
	if err != nil {
		t.Fatalf("ParseTunnelInput error: %v", err)
	}
	if !choice.Provided || len(choice.IDs) != 2 {
		t.Fatalf("unexpected choice: %+v", choice)
	}
}

func TestParseTunnelInput_Default(t *testing.T) {
	choice, err := ParseTunnelInput("", []string{"t1"})
	if err != nil {
		t.Fatalf("ParseTunnelInput error: %v", err)
	}
	if choice.Provided {
		t.Fatalf("expected Provided=false for empty input")
	}
}

func TestNextSubscriptionLabel_DefaultAndConflicts(t *testing.T) {
	if got := nextSubscriptionLabel("", map[string]struct{}{}); got != "main" {
		t.Fatalf("expected main, got %q", got)
	}

	occupied := map[string]struct{}{"main": {}}
	if got := nextSubscriptionLabel("", occupied); got != "main-1" {
		t.Fatalf("expected main-1, got %q", got)
	}

	occupied["main-1"] = struct{}{}
	if got := nextSubscriptionLabel("", occupied); got != "main-2" {
		t.Fatalf("expected main-2, got %q", got)
	}
}

func TestNextSubscriptionLabel_CustomPreferredConflict(t *testing.T) {
	if got := nextSubscriptionLabel("edge", map[string]struct{}{}); got != "edge" {
		t.Fatalf("expected edge, got %q", got)
	}

	occupied := map[string]struct{}{"edge": {}}
	if got := nextSubscriptionLabel("edge", occupied); got != "edge-1" {
		t.Fatalf("expected edge-1, got %q", got)
	}
}
