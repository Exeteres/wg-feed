//go:build windows

package installer

import (
	"strings"

	"golang.org/x/sys/windows"
)

const (
	codePageACP   = 0
	codePageOEMCP = 1
)

func decodeCommandOutput(out []byte) string {
	if len(out) == 0 {
		return ""
	}

	if s, ok := decodeBytesWithCodePage(out, codePageOEMCP); ok {
		return s
	}
	if s, ok := decodeBytesWithCodePage(out, codePageACP); ok {
		return s
	}
	return string(out)
}

func decodeBytesWithCodePage(src []byte, codePage uint32) (string, bool) {
	if len(src) == 0 {
		return "", true
	}

	needed, err := windows.MultiByteToWideChar(codePage, 0, &src[0], int32(len(src)), nil, 0)
	if err != nil || needed == 0 {
		return "", false
	}
	wide := make([]uint16, needed)
	_, err = windows.MultiByteToWideChar(codePage, 0, &src[0], int32(len(src)), &wide[0], int32(len(wide)))
	if err != nil {
		return "", false
	}
	return strings.TrimRight(windows.UTF16ToString(wide), "\x00"), true
}
