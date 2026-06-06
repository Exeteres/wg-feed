//go:build !windows

package installer

import (
	"context"
	"fmt"
)

func ensureWindowsService(_ context.Context, _ string, _ string, _ string) error {
	return fmt.Errorf("windows services are not supported on this platform")
}

func startWindowsService(_ context.Context, _ string) error {
	return fmt.Errorf("windows services are not supported on this platform")
}

func stopWindowsService(_ context.Context, _ string) error {
	return fmt.Errorf("windows services are not supported on this platform")
}

func deleteWindowsService(_ context.Context, _ string) error {
	return fmt.Errorf("windows services are not supported on this platform")
}

func windowsServiceExists(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("windows services are not supported on this platform")
}

func isWindowsServiceAlreadyRunningError(err error) bool {
	return false
}
