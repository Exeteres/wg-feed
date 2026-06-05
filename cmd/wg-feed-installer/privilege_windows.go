//go:build windows

package main

func requiresPrivilegedInstall() bool {
	// sc.exe service operations will fail with a clear permission error when not elevated
	return false
}
