package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	koanfjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	DefaultStatePath = "/var/lib/wg-feed/state.json"
	defaultConfigDir = "/etc/wg-feed"
)

var defaultConfigCandidates = []string{
	"config.yaml",
	"config.yml",
	"config.toml",
	"config.json",
}

type BackendType string

const (
	BackendWGQuick        BackendType = "wg-quick"
	BackendNetworkManager BackendType = "networkmanager"
	BackendNetNS          BackendType = "netns"
	BackendWindows        BackendType = "windows"
)

type SyncMode string

const (
	SyncModeSSE     SyncMode = "sse"
	SyncModePolling SyncMode = "polling"
)

type Config struct {
	StatePath string                `koanf:"state_path"`
	Feeds     map[string]FeedConfig `koanf:"feeds"`
}

type FeedConfig struct {
	Sync     FeedSyncConfig               `koanf:"sync"`
	Backends map[string]FeedBackendConfig `koanf:"backends"`
	Tunnels  map[string]FeedTunnelConfig  `koanf:"tunnels"`
}

type FeedTunnelConfig struct {
	Enabled *bool `koanf:"enabled"`
}

type FeedSyncConfig struct {
	Enabled   bool                  `koanf:"enabled"`
	Mode      SyncMode              `koanf:"mode"`
	Polling   FeedPollingSyncConfig `koanf:"polling"`
	Endpoints []string              `koanf:"endpoints"`
}

type FeedPollingSyncConfig struct {
	Interval int `koanf:"interval"`
}

type FeedBackendConfig struct {
	Type    BackendType                 `koanf:"type"`
	Tunnels map[string]FeedTunnelConfig `koanf:"tunnels"`
}

func Load(configPath string) (Config, error) {
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return Config{}, err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(path), parserForPath(path)); err != nil {
		return Config{}, fmt.Errorf("load config %q: %w", path, err)
	}

	raw := k.Raw()
	missing := map[string]struct{}{}
	expandedAny := expandAnyStrings(raw, missing)
	if len(missing) != 0 {
		keys := make([]string, 0, len(missing))
		for k := range missing {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return Config{}, fmt.Errorf("config references unset env vars: %s", strings.Join(keys, ", "))
	}

	var cfg Config
	expandedMap, ok := expandedAny.(map[string]any)
	if !ok {
		return Config{}, fmt.Errorf("expanded config must be an object")
	}

	kExpanded := koanf.New(".")
	if err := kExpanded.Load(confmap.Provider(expandedMap, "."), nil); err != nil {
		return Config{}, fmt.Errorf("load expanded config: %w", err)
	}
	if err := kExpanded.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := normalizeAndValidate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func resolveConfigPath(configPath string) (string, error) {
	if strings.TrimSpace(configPath) != "" {
		return strings.TrimSpace(configPath), nil
	}
	for _, name := range defaultConfigCandidates {
		candidate := filepath.Join(defaultConfigDir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no config file found in %s (looked for %s)", defaultConfigDir, strings.Join(defaultConfigCandidates, ", "))
}

func normalizeAndValidate(cfg *Config) error {
	cfg.StatePath = strings.TrimSpace(cfg.StatePath)
	if cfg.StatePath == "" {
		cfg.StatePath = DefaultStatePath
	}

	if len(cfg.Feeds) == 0 {
		return fmt.Errorf("feeds is required and must contain at least one feed")
	}

	for feedLabel, feedCfg := range cfg.Feeds {
		if strings.TrimSpace(feedLabel) == "" {
			return fmt.Errorf("feed label must not be empty")
		}

		mode := SyncMode(strings.ToLower(strings.TrimSpace(string(feedCfg.Sync.Mode))))
		if mode == "" {
			mode = SyncModeSSE
		}
		switch mode {
		case SyncModeSSE, SyncModePolling:
		default:
			return fmt.Errorf("feeds.%s.sync.mode must be one of %q or %q", feedLabel, SyncModeSSE, SyncModePolling)
		}
		feedCfg.Sync.Mode = mode

		if feedCfg.Sync.Polling.Interval < 0 {
			return fmt.Errorf("feeds.%s.sync.polling.interval must be >= 0", feedLabel)
		}

		endpoints := make([]string, 0, len(feedCfg.Sync.Endpoints))
		fragments := map[string]struct{}{}
		seen := map[string]struct{}{}
		for _, raw := range feedCfg.Sync.Endpoints {
			e := strings.TrimSpace(raw)
			if e == "" {
				continue
			}
			u, err := url.Parse(e)
			if err != nil {
				return fmt.Errorf("feeds.%s.sync.endpoints contains invalid url %q: %w", feedLabel, e, err)
			}
			if strings.ToLower(strings.TrimSpace(u.Scheme)) != "https" {
				return fmt.Errorf("feeds.%s.sync.endpoints must use https: %q", feedLabel, e)
			}
			if _, ok := seen[e]; ok {
				continue
			}
			seen[e] = struct{}{}
			endpoints = append(endpoints, e)
			frag := strings.ToLower(strings.TrimSpace(u.Fragment))
			if frag != "" {
				fragments[frag] = struct{}{}
			}
		}
		if len(endpoints) == 0 {
			return fmt.Errorf("feeds.%s.sync.endpoints must contain at least one endpoint", feedLabel)
		}
		if len(fragments) > 1 {
			return fmt.Errorf("feeds.%s.sync.endpoints must use the same URL fragment key across endpoints", feedLabel)
		}
		feedCfg.Sync.Endpoints = endpoints

		if len(feedCfg.Backends) == 0 {
			return fmt.Errorf("feeds.%s.backends must contain at least one backend", feedLabel)
		}
		for backendLabel, backendCfg := range feedCfg.Backends {
			if strings.TrimSpace(backendLabel) == "" {
				return fmt.Errorf("feeds.%s backend label must not be empty", feedLabel)
			}
			backendType := BackendType(strings.ToLower(strings.TrimSpace(string(backendCfg.Type))))
			switch backendType {
			case BackendWGQuick, BackendNetworkManager, BackendNetNS, BackendWindows:
			default:
				return fmt.Errorf("feeds.%s.backends.%s.type must be one of %q, %q, %q, %q", feedLabel, backendLabel, BackendWGQuick, BackendNetworkManager, BackendNetNS, BackendWindows)
			}
			backendCfg.Type = backendType
			for tunnelID := range backendCfg.Tunnels {
				if strings.TrimSpace(tunnelID) == "" {
					return fmt.Errorf("feeds.%s.backends.%s.tunnels key must not be empty", feedLabel, backendLabel)
				}
			}
			feedCfg.Backends[backendLabel] = backendCfg
		}

		for tunnelID := range feedCfg.Tunnels {
			if strings.TrimSpace(tunnelID) == "" {
				return fmt.Errorf("feeds.%s.tunnels key must not be empty", feedLabel)
			}
		}

		cfg.Feeds[feedLabel] = feedCfg
	}

	return nil
}

func (fc FeedConfig) DecryptURL() string {
	for _, endpoint := range fc.Sync.Endpoints {
		u, err := url.Parse(endpoint)
		if err == nil && strings.TrimSpace(u.Fragment) != "" {
			return endpoint
		}
	}
	if len(fc.Sync.Endpoints) == 0 {
		return ""
	}
	return fc.Sync.Endpoints[0]
}

func (fc FeedConfig) BackendTunnelOverrides() map[string]map[string]FeedTunnelConfig {
	if len(fc.Backends) == 0 {
		return nil
	}
	out := map[string]map[string]FeedTunnelConfig{}
	for backendLabel, backendCfg := range fc.Backends {
		if len(backendCfg.Tunnels) == 0 {
			continue
		}
		out[backendLabel] = backendCfg.Tunnels
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (fc FeedConfig) HasAnyEnabledOverride() bool {
	for _, tunnelCfg := range fc.Tunnels {
		if tunnelCfg.Enabled != nil {
			return true
		}
	}
	for _, backendCfg := range fc.Backends {
		for _, tunnelCfg := range backendCfg.Tunnels {
			if tunnelCfg.Enabled != nil {
				return true
			}
		}
	}
	return false
}

func expandAnyStrings(in any, missing map[string]struct{}) any {
	switch v := in.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			out[k] = expandAnyStrings(child, missing)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = expandAnyStrings(v[i], missing)
		}
		return out
	case string:
		return os.Expand(v, func(key string) string {
			value, ok := os.LookupEnv(key)
			if !ok {
				missing[key] = struct{}{}
				return ""
			}
			return value
		})
	default:
		return in
	}
}

func parserForPath(path string) koanf.Parser {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(path)))
	switch ext {
	case ".yml", ".yaml":
		return yaml.Parser()
	case ".json":
		return koanfjson.Parser()
	case ".toml":
		return toml.Parser()
	default:
		return yaml.Parser()
	}
}
