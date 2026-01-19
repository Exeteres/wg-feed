package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/exeteres/wg-feed/internal/stringsx"
)

type Backend string

const (
	BackendWGQuick        Backend = "wg-quick"
	BackendNetworkManager Backend = "networkmanager"
	BackendWindows        Backend = "windows"
)

type Config struct {
	Backend   Backend
	StatePath string
	SetupURLs []string
}

func FromEnv() (Config, error) {
	backend := Backend(strings.TrimSpace(os.Getenv("BACKEND")))
	switch backend {
	case BackendWGQuick, BackendNetworkManager, BackendWindows:
		// ok
	default:
		return Config{}, fmt.Errorf("BACKEND must be one of %q, %q, %q", BackendWGQuick, BackendNetworkManager, BackendWindows)
	}

	statePath := strings.TrimSpace(os.Getenv("STATE_PATH"))
	if statePath == "" {
		p, err := defaultStatePath()
		if err != nil {
			return Config{}, err
		}
		statePath = p
	}

	setupURLs, err := parseSetupURLsFromEnv()
	if err != nil {
		return Config{}, err
	}

	return Config{Backend: backend, StatePath: statePath, SetupURLs: setupURLs}, nil
}

func defaultStatePath() (string, error) {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		base := strings.TrimSpace(os.Getenv("APPDATA"))
		if base == "" {
			if home == "" {
				return "", errors.New("cannot determine state path: APPDATA and HOME are empty")
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "wg-feed", "state.json"), nil
	case "darwin":
		if home == "" {
			return "", errors.New("cannot determine state path: HOME is empty")
		}
		return filepath.Join(home, "Library", "Application Support", "wg-feed", "state.json"), nil
	default:
		base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
		if base == "" {
			if home == "" {
				return "", errors.New("cannot determine state path: XDG_STATE_HOME and HOME are empty")
			}
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, "wg-feed", "state.json"), nil
	}
}

func parseSetupURLsFromEnv() ([]string, error) {
	raw := strings.TrimSpace(os.Getenv("SETUP_URLS"))
	if raw == "" {
		return nil, errors.New("SETUP_URLS is required (comma-separated list of setup URLs)")
	}

	urls := stringsx.SplitCommaSeparated(raw)
	if len(urls) == 0 {
		return nil, errors.New("SETUP_URLS is required (comma-separated list of setup URLs)")
	}
	return urls, nil
}
