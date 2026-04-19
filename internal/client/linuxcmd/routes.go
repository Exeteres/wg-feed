package linuxcmd

import (
	"context"
	"net/netip"

	"github.com/exeteres/wg-feed/internal/client/execx"
)

func RouteReplace(ctx context.Context, runner execx.Runner, iface string, p netip.Prefix, namespace string) error {
	args := routeArgs("replace", iface, p, namespace)
	_, err := runner.Run(ctx, "ip", args...)
	return err
}

func RouteDelete(ctx context.Context, runner execx.Runner, iface string, p netip.Prefix, namespace string) error {
	args := routeArgs("del", iface, p, namespace)
	_, err := runner.Run(ctx, "ip", args...)
	return err
}

func routeArgs(action string, iface string, p netip.Prefix, namespace string) []string {
	args := make([]string, 0, 9)
	if p.Addr().Is6() {
		args = append(args, "-6")
	} else {
		args = append(args, "-4")
	}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	args = append(args, "route", action, p.String(), "dev", iface)
	return args
}
