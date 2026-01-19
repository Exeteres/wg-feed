package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/exeteres/wg-feed/internal/etcd"
)

type Config struct {
	ServerPort    string
	EtcdEndpoints []string
}

func FromEnv() (Config, error) {
	port := strings.TrimSpace(os.Getenv("SERVER_PORT"))
	if port == "" {
		port = "8080"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return Config{}, fmt.Errorf("SERVER_PORT must be an integer: %w", err)
	}

	endpoints, err := etcd.EndpointsFromEnv()
	if err != nil {
		return Config{}, err
	}

	return Config{
		ServerPort:    port,
		EtcdEndpoints: endpoints,
	}, nil
}
