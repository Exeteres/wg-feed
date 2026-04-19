package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/exeteres/wg-feed/internal/installer"
)

func main() {
	releaseTag := strings.TrimSpace(os.Getenv("WG_FEED_VERSION"))
	if releaseTag == "" {
		fmt.Fprintln(os.Stderr, "WG_FEED_VERSION environment variable is required")
		os.Exit(1)
	}
	daemonChecksum := strings.TrimSpace(os.Getenv("WG_FEED_DAEMON_CHECKSUM"))
	if daemonChecksum == "" {
		fmt.Fprintln(os.Stderr, "WG_FEED_DAEMON_CHECKSUM environment variable is required")
		os.Exit(1)
	}

	configPath := flag.String("config", installer.DefaultConfigPath, "path to wg-feed config file")
	unitPath := flag.String("unit", installer.DefaultUnitPath, "path to systemd unit file")
	daemonPath := flag.String("daemon", installer.DefaultDaemonPath, "path to install wg-feed-daemon binary")
	flag.Parse()

	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "wg-feed-installer must run as root")
		os.Exit(1)
	}
	opts := installer.NormalizeApplyOptions(installer.ApplyOptions{
		ConfigPath:     *configPath,
		UnitPath:       *unitPath,
		DaemonPath:     *daemonPath,
		ReleaseTag:     releaseTag,
		DaemonChecksum: daemonChecksum,
	})
	if err := runInstallerApp(opts); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
