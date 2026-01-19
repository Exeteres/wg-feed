package config

import "testing"

func TestFromEnv_Valid(t *testing.T) {
	t.Setenv("BACKEND", string(BackendWGQuick))
	t.Setenv("STATE_PATH", "/tmp/state.json")
	t.Setenv("SETUP_URLS", " https://a.example , https://b.example ")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Backend != BackendWGQuick {
		t.Fatalf("unexpected backend: %q", cfg.Backend)
	}
	if cfg.StatePath != "/tmp/state.json" {
		t.Fatalf("unexpected state path: %q", cfg.StatePath)
	}
	if len(cfg.SetupURLs) != 2 || cfg.SetupURLs[0] != "https://a.example" || cfg.SetupURLs[1] != "https://b.example" {
		t.Fatalf("unexpected setup urls: %#v", cfg.SetupURLs)
	}
}

func TestFromEnv_InvalidBackend(t *testing.T) {
	t.Setenv("BACKEND", "nope")
	t.Setenv("SETUP_URLS", "https://a.example")
	_, err := FromEnv()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseSetupURLsFromEnv_Required(t *testing.T) {
	t.Setenv("SETUP_URLS", "")
	_, err := parseSetupURLsFromEnv()
	if err == nil {
		t.Fatalf("expected error")
	}
}
