//go:build !windows

package main

import "os"

func requiresPrivilegedInstall() bool {
	return os.Geteuid() != 0
}
