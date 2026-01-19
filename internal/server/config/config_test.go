package config

import "testing"

func TestFromEnv_DefaultPort(t *testing.T) {
	t.Setenv("SERVER_PORT", "")
	t.Setenv("ETCD_ENDPOINTS", "http://127.0.0.1:2379")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServerPort != "8080" {
		t.Fatalf("unexpected port: %q", cfg.ServerPort)
	}
	if len(cfg.EtcdEndpoints) != 1 || cfg.EtcdEndpoints[0] != "http://127.0.0.1:2379" {
		t.Fatalf("unexpected endpoints: %#v", cfg.EtcdEndpoints)
	}
}

func TestFromEnv_InvalidPort(t *testing.T) {
	t.Setenv("SERVER_PORT", "nope")
	t.Setenv("ETCD_ENDPOINTS", "http://127.0.0.1:2379")
	_, err := FromEnv()
	if err == nil {
		t.Fatalf("expected error")
	}
}
