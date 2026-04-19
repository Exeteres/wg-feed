package installer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigPath = "/etc/wg-feed/config.yaml"
	DefaultUnitPath   = "/etc/systemd/system/wg-feed-daemon.service"
	DefaultDaemonPath = "/usr/local/bin/wg-feed-daemon"
)

type ApplyOptions struct {
	ConfigPath     string
	UnitPath       string
	DaemonPath     string
	ReleaseTag     string
	DaemonChecksum string
	// DownloadProgress receives daemon download progress in bytes.
	// totalBytes may be <= 0 when unknown.
	DownloadProgress func(downloadedBytes, totalBytes int64)
}

type DaemonStatus string

const (
	DaemonStatusOK       DaemonStatus = "ok"
	DaemonStatusOutdated DaemonStatus = "outdated"
	DaemonStatusMissing  DaemonStatus = "missing"
)

func NormalizeApplyOptions(opts ApplyOptions) ApplyOptions {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = DefaultConfigPath
	}
	if strings.TrimSpace(opts.UnitPath) == "" {
		opts.UnitPath = DefaultUnitPath
	}
	if strings.TrimSpace(opts.DaemonPath) == "" {
		opts.DaemonPath = DefaultDaemonPath
	}
	return opts
}

func LoadConfigOrEmpty(configPath string) (config.Config, error) {
	if strings.TrimSpace(configPath) == "" {
		configPath = DefaultConfigPath
	}
	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.Config{StatePath: config.DefaultStatePath, Feeds: map[string]config.FeedConfig{}}, nil
		}
		return config.Config{}, err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return config.Config{}, err
	}
	if cfg.Feeds == nil {
		cfg.Feeds = map[string]config.FeedConfig{}
	}
	if strings.TrimSpace(cfg.StatePath) == "" {
		cfg.StatePath = config.DefaultStatePath
	}
	return cfg, nil
}

func LoadExistingSnapshot(configPath string) (ExistingSnapshot, error) {
	if strings.TrimSpace(configPath) == "" {
		configPath = DefaultConfigPath
	}
	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ExistingSnapshot{ByURL: map[string]ExistingSubscription{}}, nil
		}
		return ExistingSnapshot{}, err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return ExistingSnapshot{}, err
	}
	return InferExistingSnapshot(cfg), nil
}

func InstallDaemonBinary(ctx context.Context, daemonPath string, releaseTag string, onProgress func(downloadedBytes, totalBytes int64)) error {
	if strings.TrimSpace(daemonPath) == "" {
		daemonPath = DefaultDaemonPath
	}
	releaseTag = strings.TrimSpace(releaseTag)
	if releaseTag == "" {
		return fmt.Errorf("missing release tag for daemon download")
	}
	arch, err := detectReleaseArch()
	if err != nil {
		return err
	}
	asset := fmt.Sprintf("wg-feed-daemon_linux_%s", arch)
	url := fmt.Sprintf("https://github.com/exeteres/wg-feed/releases/download/%s/%s", releaseTag, asset)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download daemon binary: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("close daemon download response body: %v", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		snippet := strings.TrimSpace(string(body))
		if strings.Contains(strings.ToLower(contentType), "text/html") {
			snippet = "received HTML page instead of daemon binary"
		} else if strings.HasPrefix(snippet, "<") {
			snippet = "received unexpected non-binary response"
		}
		return fmt.Errorf("download daemon binary %q (%s): unexpected status %d: %s", asset, releaseTag, resp.StatusCode, snippet)
	}

	tmpFile, err := os.CreateTemp("", "wg-feed-daemon-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if onProgress != nil {
		onProgress(0, resp.ContentLength)
	}
	written, err := io.Copy(tmpFile, &downloadProgressReader{r: resp.Body, total: resp.ContentLength, onProgress: onProgress})
	if err != nil {
		_ = tmpFile.Close()
		return err
	}
	if onProgress != nil {
		onProgress(written, resp.ContentLength)
	}
	if err := tmpFile.Chmod(0o755); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(daemonPath), 0o755); err != nil {
		return err
	}
	return os.Rename(tmpName, daemonPath)
}

func ApplySystem(ctx context.Context, plan InstallPlan, feedDocs map[string]model.FeedDocument, opts ApplyOptions) error {
	opts = NormalizeApplyOptions(opts)

	cfg, err := BuildConfig(plan, feedDocs)
	if err != nil {
		return err
	}
	return ApplyConfigSystem(ctx, cfg, opts)
}

func ApplyConfigSystem(ctx context.Context, cfg config.Config, opts ApplyOptions) error {
	opts = NormalizeApplyOptions(opts)
	cfgBytes, err := marshalYAMLWithIndent(cfg, 2)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(opts.ConfigPath, cfgBytes, 0o644); err != nil {
		return err
	}

	unitText := BuildSystemdUnit(opts.ConfigPath, HasNetNSBackendConfig(cfg))
	if err := writeFileAtomic(opts.UnitPath, []byte(unitText), 0o644); err != nil {
		return err
	}

	if strings.TrimSpace(opts.ReleaseTag) == "" {
		return fmt.Errorf("ReleaseTag is required")
	}
	if strings.TrimSpace(opts.DaemonChecksum) == "" {
		return fmt.Errorf("DaemonChecksum is required")
	}
	if err := InstallDaemonBinary(ctx, opts.DaemonPath, opts.ReleaseTag, opts.DownloadProgress); err != nil {
		return err
	}
	if err := verifyFileSHA256(opts.DaemonPath, opts.DaemonChecksum); err != nil {
		return fmt.Errorf("verify daemon checksum: %w", err)
	}

	if err := runSystemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if err := runSystemctl(ctx, "enable", "wg-feed-daemon"); err != nil {
		return err
	}
	if err := runSystemctl(ctx, "restart", "wg-feed-daemon"); err != nil {
		if err2 := runSystemctl(ctx, "start", "wg-feed-daemon"); err2 != nil {
			return err
		}
	}
	return nil
}

func marshalYAMLWithIndent(v any, indent int) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func HasNetNSBackendConfig(cfg config.Config) bool {
	for _, fc := range cfg.Feeds {
		for _, backendCfg := range fc.Backends {
			if backendCfg.Type == config.BackendNetNS {
				return true
			}
		}
	}
	return false
}

func DetectInstallTraces(ctx context.Context, opts ApplyOptions) bool {
	opts = NormalizeApplyOptions(opts)
	for _, path := range []string{opts.ConfigPath, opts.UnitPath, opts.DaemonPath} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	if err := runSystemctl(ctx, "is-enabled", "wg-feed-daemon"); err == nil {
		return true
	}
	if err := runSystemctl(ctx, "status", "wg-feed-daemon"); err == nil {
		return true
	}
	return false
}

func CurrentDaemonStatus(opts ApplyOptions) (DaemonStatus, string, error) {
	opts = NormalizeApplyOptions(opts)
	expected := normalizeChecksum(opts.DaemonChecksum)
	if expected == "" {
		return DaemonStatusOutdated, "", fmt.Errorf("DaemonChecksum is required")
	}
	sum, err := fileSHA256(opts.DaemonPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DaemonStatusMissing, "", nil
		}
		return DaemonStatusOutdated, "", err
	}
	if sum == expected {
		return DaemonStatusOK, sum, nil
	}
	return DaemonStatusOutdated, sum, nil
}

func UninstallSystem(ctx context.Context, opts ApplyOptions) error {
	opts = NormalizeApplyOptions(opts)

	_ = runSystemctl(ctx, "stop", "wg-feed-daemon")
	_ = runSystemctl(ctx, "disable", "wg-feed-daemon")

	for _, path := range []string{opts.ConfigPath, opts.UnitPath, opts.DaemonPath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if err := runSystemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	return nil
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func runSystemctl(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func detectReleaseArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported arch %q", runtime.GOARCH)
	}
}

type downloadProgressReader struct {
	r          io.Reader
	total      int64
	read       int64
	onProgress func(downloadedBytes, totalBytes int64)
}

func (p *downloadProgressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.read += int64(n)
		if p.onProgress != nil {
			p.onProgress(p.read, p.total)
		}
	}
	return n, err
}

func normalizeChecksum(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			log.Printf("close file %q: %v", path, cerr)
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verifyFileSHA256(path string, expected string) error {
	expected = normalizeChecksum(expected)
	if expected == "" {
		return fmt.Errorf("missing expected checksum")
	}
	actual, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("checksum mismatch: got %s want %s", actual, expected)
	}
	return nil
}
