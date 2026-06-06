package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCurrentDaemonStatus(t *testing.T) {
	tmp := t.TempDir()
	daemonPath := filepath.Join(tmp, "wg-feed-daemon")
	content := []byte("daemon-bytes")
	if err := os.WriteFile(daemonPath, content, 0o755); err != nil {
		t.Fatalf("write daemon: %v", err)
	}

	checksum := sha256Hex(content)

	status, sum, err := CurrentDaemonStatus(ApplyOptions{DaemonPath: daemonPath, DaemonChecksum: strings.ToUpper(checksum)})
	if err != nil {
		t.Fatalf("CurrentDaemonStatus OK: %v", err)
	}
	if status != DaemonStatusOK {
		t.Fatalf("status=%s, want %s", status, DaemonStatusOK)
	}
	if sum != checksum {
		t.Fatalf("sum=%s, want %s", sum, checksum)
	}

	status, _, err = CurrentDaemonStatus(ApplyOptions{DaemonPath: daemonPath, DaemonChecksum: sha256Hex([]byte("other"))})
	if err != nil {
		t.Fatalf("CurrentDaemonStatus outdated: %v", err)
	}
	if status != DaemonStatusOutdated {
		t.Fatalf("status=%s, want %s", status, DaemonStatusOutdated)
	}

	status, _, err = CurrentDaemonStatus(ApplyOptions{DaemonPath: filepath.Join(tmp, "missing"), DaemonChecksum: checksum})
	if err != nil {
		t.Fatalf("CurrentDaemonStatus missing: %v", err)
	}
	if status != DaemonStatusMissing {
		t.Fatalf("status=%s, want %s", status, DaemonStatusMissing)
	}

	_, _, err = CurrentDaemonStatus(ApplyOptions{DaemonPath: daemonPath})
	if err == nil || !strings.Contains(err.Error(), "DaemonChecksum is required") {
		t.Fatalf("missing checksum error=%v, want required error", err)
	}
}

func TestApplyConfigSystem_SkipsDaemonDownloadWhenChecksumMatches(t *testing.T) {
	setupFakeSystemctl(t)

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	unitPath := filepath.Join(tmp, "wg-feed-daemon.service")
	daemonPath := filepath.Join(tmp, "wg-feed-daemon")
	daemonBytes := []byte("already-installed-daemon")
	if err := os.WriteFile(daemonPath, daemonBytes, 0o755); err != nil {
		t.Fatalf("write daemon: %v", err)
	}

	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("unexpected download request: %s", req.URL.String())
	})}
	t.Cleanup(func() { http.DefaultClient = origClient })

	opts := ApplyOptions{
		ConfigPath:     configPath,
		UnitPath:       unitPath,
		DaemonPath:     daemonPath,
		ReleaseTag:     "v1.2.3",
		DaemonChecksum: sha256Hex(daemonBytes),
	}

	if err := ApplyConfigSystem(context.Background(), sampleConfig(config.BackendWGQuick), opts); err != nil {
		t.Fatalf("ApplyConfigSystem: %v", err)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(configContent), "state_path:") {
		t.Fatalf("config missing canonical state_path key: %s", string(configContent))
	}
	if strings.Contains(string(configContent), "StatePath:") {
		t.Fatalf("config should not contain Go struct field key StatePath")
	}
	unitContent, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("unit not written: %v", err)
	}
	if !strings.Contains(string(unitContent), "ExecStart=/usr/local/bin/wg-feed-daemon --config "+configPath) {
		t.Fatalf("unit missing config path")
	}

	got, err := os.ReadFile(daemonPath)
	if err != nil {
		t.Fatalf("read daemon: %v", err)
	}
	if string(got) != string(daemonBytes) {
		t.Fatalf("daemon content changed unexpectedly")
	}

	lines := readNonEmptyLines(t, mustGetenv(t, "WG_TEST_SYSTEMCTL_LOG"))
	want := []string{
		"daemon-reload",
		"enable wg-feed-daemon",
		"restart wg-feed-daemon",
	}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("systemctl sequence=%v, want %v", lines, want)
	}
}

func TestApplyConfigSystem_DownloadsWhenMissing_AndFallsBackToStart(t *testing.T) {
	setupFakeSystemctl(t)
	t.Setenv("WG_TEST_FAIL_RESTART", "1")

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	unitPath := filepath.Join(tmp, "wg-feed-daemon.service")
	daemonPath := filepath.Join(tmp, "wg-feed-daemon")
	daemonBytes := []byte("downloaded-daemon")
	checksum := sha256Hex(daemonBytes)

	asset, err := daemonReleaseAssetName("v1.2.3")
	if err != nil {
		t.Fatalf("daemonReleaseAssetName: %v", err)
	}
	wantSuffix := "/releases/download/v1.2.3/" + asset

	origClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "github.com" {
			t.Fatalf("unexpected host: %s", req.URL.Host)
		}
		if !strings.HasSuffix(req.URL.Path, wantSuffix) {
			t.Fatalf("unexpected path: %s, want suffix %s", req.URL.Path, wantSuffix)
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: int64(len(daemonBytes)),
			Body:          io.NopCloser(strings.NewReader(string(daemonBytes))),
			Header:        make(http.Header),
		}, nil
	})}
	t.Cleanup(func() { http.DefaultClient = origClient })

	opts := ApplyOptions{
		ConfigPath:     configPath,
		UnitPath:       unitPath,
		DaemonPath:     daemonPath,
		ReleaseTag:     "v1.2.3",
		DaemonChecksum: checksum,
	}

	if err := ApplyConfigSystem(context.Background(), sampleConfig(config.BackendNetNS), opts); err != nil {
		t.Fatalf("ApplyConfigSystem: %v", err)
	}

	got, err := os.ReadFile(daemonPath)
	if err != nil {
		t.Fatalf("read downloaded daemon: %v", err)
	}
	if string(got) != string(daemonBytes) {
		t.Fatalf("downloaded daemon mismatch")
	}
	st, err := os.Stat(daemonPath)
	if err != nil {
		t.Fatalf("stat daemon: %v", err)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Fatalf("daemon is not executable: mode=%o", st.Mode().Perm())
	}

	unitContent, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	if !strings.Contains(string(unitContent), "CAP_SYS_ADMIN") {
		t.Fatalf("netns unit missing CAP_SYS_ADMIN")
	}

	lines := readNonEmptyLines(t, mustGetenv(t, "WG_TEST_SYSTEMCTL_LOG"))
	want := []string{
		"daemon-reload",
		"enable wg-feed-daemon",
		"restart wg-feed-daemon",
		"start wg-feed-daemon",
	}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("systemctl sequence=%v, want %v", lines, want)
	}
}

func TestApplyConfigSystem_ValidatesRequiredReleaseFields(t *testing.T) {
	tmp := t.TempDir()
	daemonPath := filepath.Join(tmp, "wg-feed-daemon")

	tests := []struct {
		name    string
		opts    ApplyOptions
		wantErr string
	}{
		{
			name: "missing release tag",
			opts: ApplyOptions{
				ConfigPath:     filepath.Join(tmp, "config1.yaml"),
				UnitPath:       filepath.Join(tmp, "unit1.service"),
				DaemonPath:     daemonPath,
				DaemonChecksum: "abc",
			},
			wantErr: "ReleaseTag is required",
		},
		{
			name: "missing daemon checksum",
			opts: ApplyOptions{
				ConfigPath: filepath.Join(tmp, "config2.yaml"),
				UnitPath:   filepath.Join(tmp, "unit2.service"),
				DaemonPath: daemonPath,
				ReleaseTag: "v1.2.3",
			},
			wantErr: "DaemonChecksum is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ApplyConfigSystem(context.Background(), sampleConfig(config.BackendWGQuick), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

func TestDetectInstallTraces(t *testing.T) {
	setupFakeSystemctl(t)
	ctx := context.Background()
	tmp := t.TempDir()
	opts := ApplyOptions{
		ConfigPath: filepath.Join(tmp, "config.yaml"),
		UnitPath:   filepath.Join(tmp, "wg-feed-daemon.service"),
		DaemonPath: filepath.Join(tmp, "wg-feed-daemon"),
	}

	if got := DetectInstallTraces(ctx, opts); got {
		t.Fatalf("DetectInstallTraces()=true, want false when nothing exists")
	}

	if err := os.WriteFile(opts.ConfigPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if got := DetectInstallTraces(ctx, opts); !got {
		t.Fatalf("DetectInstallTraces()=false, want true when config exists")
	}

	if err := os.Remove(opts.ConfigPath); err != nil {
		t.Fatalf("remove config: %v", err)
	}
	t.Setenv("WG_TEST_SYSTEMCTL_IS_ENABLED_OK", "1")
	if got := DetectInstallTraces(ctx, opts); !got {
		t.Fatalf("DetectInstallTraces()=false, want true when systemctl is-enabled succeeds")
	}
}

func TestUninstallSystem_RemovesFilesAndReloadsSystemd(t *testing.T) {
	setupFakeSystemctl(t)
	ctx := context.Background()
	tmp := t.TempDir()
	opts := ApplyOptions{
		ConfigPath: filepath.Join(tmp, "config.yaml"),
		UnitPath:   filepath.Join(tmp, "wg-feed-daemon.service"),
		DaemonPath: filepath.Join(tmp, "wg-feed-daemon"),
	}

	for _, path := range []string{opts.ConfigPath, opts.UnitPath, opts.DaemonPath} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	if err := UninstallSystem(ctx, opts); err != nil {
		t.Fatalf("UninstallSystem: %v", err)
	}

	for _, path := range []string{opts.ConfigPath, opts.UnitPath, opts.DaemonPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("path %s still exists, err=%v", path, err)
		}
	}

	lines := readNonEmptyLines(t, mustGetenv(t, "WG_TEST_SYSTEMCTL_LOG"))
	want := []string{
		"stop wg-feed-daemon",
		"disable wg-feed-daemon",
		"daemon-reload",
	}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("systemctl sequence=%v, want %v", lines, want)
	}
}

func sampleConfig(backendType config.BackendType) config.Config {
	return config.Config{
		StatePath: filepath.Join("/var", "lib", "wg-feed", "state.json"),
		Feeds: map[string]config.FeedConfig{
			"main": {
				Sync: config.FeedSyncConfig{
					Enabled:   true,
					Mode:      config.SyncModeSSE,
					Endpoints: []string{"https://example.test/subscription"},
				},
				Backends: map[string]config.FeedBackendConfig{
					"default": {
						Type: backendType,
					},
				},
				Tunnels: map[string]config.FeedTunnelConfig{},
			},
		},
	}
}

func setupFakeSystemctl(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "systemctl.log")
	binPath := filepath.Join(tmp, "systemctl")

	script := `#!/bin/sh
set -eu
echo "$*" >> "$WG_TEST_SYSTEMCTL_LOG"

if [ "${1:-}" = "restart" ] && [ "${WG_TEST_FAIL_RESTART:-}" = "1" ]; then
  echo "restart failed" >&2
  exit 1
fi

if [ "${1:-}" = "is-enabled" ]; then
  if [ "${WG_TEST_SYSTEMCTL_IS_ENABLED_OK:-}" = "1" ]; then
    exit 0
  fi
  exit 1
fi

if [ "${1:-}" = "status" ]; then
  if [ "${WG_TEST_SYSTEMCTL_STATUS_OK:-}" = "1" ]; then
    exit 0
  fi
  exit 1
fi

exit 0
`

	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}

	t.Setenv("WG_TEST_SYSTEMCTL_LOG", logPath)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if runtime.GOOS == "windows" {
		t.Skip("systemctl path stubbing tests require POSIX shell")
	}
}

func readNonEmptyLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	parts := strings.Split(strings.TrimSpace(string(b)), "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func mustGetenv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("missing env %s", key)
	}
	return v
}

func TestDefaultPathsForGOOS(t *testing.T) {
	t.Parallel()

	if got := defaultConfigPathForGOOS("windows", "C:\\Program Files"); got != filepath.Join("C:\\Program Files", "wg-feed", "config.yaml") {
		t.Fatalf("windows config path=%q", got)
	}
	if got := defaultDaemonPathForGOOS("windows", "C:\\Program Files"); got != filepath.Join("C:\\Program Files", "wg-feed", "wg-feed-daemon.exe") {
		t.Fatalf("windows daemon path=%q", got)
	}
	if got := defaultStatePathForGOOS("windows", "C:\\Program Files"); got != filepath.Join("C:\\Program Files", "wg-feed", "state.json") {
		t.Fatalf("windows state path=%q", got)
	}
	if got := defaultUnitPathForGOOS("windows"); got != "" {
		t.Fatalf("windows unit path=%q, want empty", got)
	}

	if got := defaultConfigPathForGOOS("linux", ""); got != "/etc/wg-feed/config.yaml" {
		t.Fatalf("linux config path=%q", got)
	}
	if got := defaultDaemonPathForGOOS("linux", ""); got != "/usr/local/bin/wg-feed-daemon" {
		t.Fatalf("linux daemon path=%q", got)
	}
	if got := defaultStatePathForGOOS("linux", ""); got != config.DefaultStatePath {
		t.Fatalf("linux state path=%q", got)
	}
	if got := defaultUnitPathForGOOS("linux"); got != "/etc/systemd/system/wg-feed-daemon.service" {
		t.Fatalf("linux unit path=%q", got)
	}
}
