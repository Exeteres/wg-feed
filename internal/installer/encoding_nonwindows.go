//go:build !windows

package installer

func decodeCommandOutput(out []byte) string {
	return string(out)
}
