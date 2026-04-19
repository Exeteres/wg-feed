package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ConfigFile_Valid(t *testing.T) {
	t.Setenv("SUB_URL", "https://a.example/sub#abc")
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`{
	"state_path": "/tmp/state.json",
	"feeds": {
		"feed1": {
			"sync": {
				"enabled": true,
				"mode": "sse",
				"polling": {
					"interval": 0
				},
				"endpoints": [
					"$SUB_URL"
				]
			},
			"backends": {
				"b1": {
					"type": "wg-quick",
					"tunnels": {
						"tunnel-a": {
							"enabled": true
						}
					}
				},
				"b2": {
					"type": "networkmanager"
				},
				"b3": {
					"type": "netns"
				}
			},
			"tunnels": {
				"tunnel-a": {
					"enabled": false
				}
			}
		}
	}
}`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.StatePath != "/tmp/state.json" {
		t.Fatalf("unexpected state path: %q", cfg.StatePath)
	}
	f := cfg.Feeds["feed1"]
	if !f.Sync.Enabled {
		t.Fatalf("expected sync enabled")
	}
	if f.Sync.Mode != SyncModeSSE {
		t.Fatalf("unexpected mode: %q", f.Sync.Mode)
	}
	if len(f.Sync.Endpoints) != 1 || f.Sync.Endpoints[0] != "https://a.example/sub#abc" {
		t.Fatalf("unexpected endpoints: %#v", f.Sync.Endpoints)
	}
	if f.Backends["b1"].Type != BackendWGQuick {
		t.Fatalf("unexpected backend type: %q", f.Backends["b1"].Type)
	}
	if f.Backends["b2"].Type != BackendNetworkManager {
		t.Fatalf("unexpected backend type: %q", f.Backends["b2"].Type)
	}
	if f.Backends["b3"].Type != BackendNetNS {
		t.Fatalf("unexpected backend type: %q", f.Backends["b3"].Type)
	}
	if f.Tunnels["tunnel-a"].Enabled == nil || *f.Tunnels["tunnel-a"].Enabled {
		t.Fatalf("expected tunnel-a enabled override=false")
	}
	if f.Backends["b1"].Tunnels["tunnel-a"].Enabled == nil || !*f.Backends["b1"].Tunnels["tunnel-a"].Enabled {
		t.Fatalf("expected backend tunnel-a enabled override=true")
	}
}

func TestLoad_StatePathDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`{
	"feeds": {
		"feed1": {
			"sync": {
				"enabled": true,
				"endpoints": [
					"https://a.example/sub"
				]
			},
			"backends": {
				"b1": {
					"type": "wg-quick"
				}
			}
		}
	}
}`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StatePath != DefaultStatePath {
		t.Fatalf("unexpected default state path: %q", cfg.StatePath)
	}
	if cfg.Feeds["feed1"].Sync.Mode != SyncModeSSE {
		t.Fatalf("expected default sync mode sse")
	}
}

func TestLoad_MissingEnv_Fails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`{
	"feeds": {
		"feed1": {
			"sync": {
				"enabled": true,
				"endpoints": [
					"$MISSING_ENV"
				]
			},
			"backends": {
				"b1": {
					"type": "wg-quick"
				}
			}
		}
	}
}`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoad_DifferentFragments_Fails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`{
	"feeds": {
		"feed1": {
			"sync": {
				"enabled": true,
				"endpoints": [
					"https://a.example/sub#aaa",
					"https://b.example/sub#bbb"
				]
			},
			"backends": {
				"b1": {
					"type": "wg-quick"
				}
			}
		}
	}
}`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoad_EmptyTunnelKey_Fails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`{
	"feeds": {
		"feed1": {
			"sync": {
				"enabled": true,
				"endpoints": [
					"https://a.example/sub"
				]
			},
			"backends": {
				"b1": {
					"type": "wg-quick"
				}
			},
			"tunnels": {
				"": {
					"enabled": true
				}
			}
		}
	}
}`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoad_EmptyBackendTunnelKey_Fails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`{
	"feeds": {
		"feed1": {
			"sync": {
				"enabled": true,
				"endpoints": [
					"https://a.example/sub"
				]
			},
			"backends": {
				"b1": {
					"type": "wg-quick",
					"tunnels": {
						"": {
							"enabled": true
						}
					}
				}
			}
		}
	}
}`), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected error")
	}
}
