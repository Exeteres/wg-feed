package namegen

import (
	"fmt"
	"strconv"
	"strings"
)

// EffectiveName returns an effective backend object name from a requested hint.
// It appends numeric suffixes (-1, -2, ...) when a collision checker reports the
// current candidate as occupied.
func EffectiveName(requested string, isOccupied func(string) (bool, error)) (string, error) {
	base := strings.TrimSpace(requested)
	if base == "" {
		return "", fmt.Errorf("requested name is empty")
	}
	prefix, start, hasNumericSuffix := splitTrailingNumber(base)

	for i := 0; i < 10000; i++ {
		candidate := base
		if hasNumericSuffix {
			if i > 0 {
				candidate = prefix + strconv.Itoa(start+i)
			}
		} else if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		occupied := false
		if isOccupied != nil {
			ok, err := isOccupied(candidate)
			if err == nil {
				occupied = ok
			}
		}
		if !occupied {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("unable to allocate unique name for %q", base)
}

func splitTrailingNumber(name string) (prefix string, number int, ok bool) {
	i := len(name) - 1
	for i >= 0 {
		c := name[i]
		if c < '0' || c > '9' {
			break
		}
		i--
	}
	if i == len(name)-1 {
		return "", 0, false
	}
	n, err := strconv.Atoi(name[i+1:])
	if err != nil {
		return "", 0, false
	}
	return name[:i+1], n, true
}
