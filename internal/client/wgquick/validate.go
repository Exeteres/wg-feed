package wgquick

import (
	"errors"
	"strings"
)

var (
	ErrMissingPrivateKey = errors.New("wg-quick config missing [Interface] PrivateKey")
	ErrMissingPeer       = errors.New("wg-quick config missing at least one [Peer]")
)

func ValidateRequired(cfg Config) error {
	if strings.TrimSpace(cfg.Interface.PrivateKey) == "" {
		return ErrMissingPrivateKey
	}
	if len(cfg.Peers) == 0 {
		return ErrMissingPeer
	}
	return nil
}
