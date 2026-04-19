package linuxcmd

import (
	"context"

	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/wgquick"
)

type CommandSet struct {
	WG       string
	WGQuick  string
	LinkType string
}

var (
	DefaultCommands = CommandSet{WG: "wg", WGQuick: "wg-quick", LinkType: "wireguard"}
	AmneziaCommands = CommandSet{WG: "awg", WGQuick: "awg-quick", LinkType: "amneziawg"}
)

func CommandSetForConfig(wgQuickConfig string) CommandSet {
	if wgquick.HasAmneziaExtensions(wgQuickConfig) {
		return AmneziaCommands
	}
	return DefaultCommands
}

func IsUp(ctx context.Context, runner execx.Runner, iface string, commands CommandSet) bool {
	_, err := runner.Run(ctx, commands.WG, "show", iface)
	return err == nil
}
