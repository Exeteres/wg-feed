package etcd

import (
	"errors"
	"os"
	"strings"

	"github.com/exeteres/wg-feed/internal/stringsx"
)

func EndpointsFromEnv() ([]string, error) {
	rawEndpoints := strings.TrimSpace(os.Getenv("ETCD_ENDPOINTS"))
	if rawEndpoints == "" {
		return nil, errors.New("ETCD_ENDPOINTS is required")
	}

	endpoints := stringsx.SplitCommaSeparated(rawEndpoints)
	if len(endpoints) == 0 {
		return nil, errors.New("ETCD_ENDPOINTS is required")
	}
	return endpoints, nil
}
