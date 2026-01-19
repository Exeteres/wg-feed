package wgquick

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/exeteres/wg-feed/internal/stringsx"
)

type Config struct {
	Interface Interface
	Peers     []Peer
}

type Interface struct {
	PrivateKey string
	Addresses  []string
	DNS        []string
	MTU        *int
}

type Peer struct {
	PublicKey           string
	PresharedKey        string
	Endpoint            string
	AllowedIPs          []string
	PersistentKeepalive *int
}

func Parse(data []byte) (Config, error) {
	var cfg Config

	s := bufio.NewScanner(strings.NewReader(string(data)))
	section := ""
	var currentPeer *Peer

	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")))
			if section == "peer" {
				cfg.Peers = append(cfg.Peers, Peer{})
				currentPeer = &cfg.Peers[len(cfg.Peers)-1]
			} else {
				currentPeer = nil
			}
			continue
		}

		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)

		switch section {
		case "interface":
			switch key {
			case "privatekey":
				cfg.Interface.PrivateKey = val
			case "address":
				cfg.Interface.Addresses = append(cfg.Interface.Addresses, stringsx.SplitCommaSeparated(val)...)
			case "dns":
				cfg.Interface.DNS = append(cfg.Interface.DNS, stringsx.SplitCommaSeparated(val)...)
			case "mtu":
				i, err := strconv.Atoi(val)
				if err != nil {
					return Config{}, fmt.Errorf("invalid MTU %q", val)
				}
				cfg.Interface.MTU = &i
			}
		case "peer":
			if currentPeer == nil {
				continue
			}
			switch key {
			case "publickey":
				currentPeer.PublicKey = val
			case "presharedkey":
				currentPeer.PresharedKey = val
			case "endpoint":
				currentPeer.Endpoint = val
			case "allowedips":
				currentPeer.AllowedIPs = append(currentPeer.AllowedIPs, stringsx.SplitCommaSeparated(val)...)
			case "persistentkeepalive":
				i, err := strconv.Atoi(val)
				if err != nil {
					return Config{}, fmt.Errorf("invalid PersistentKeepalive %q", val)
				}
				currentPeer.PersistentKeepalive = &i
			}
		}
	}
	if err := s.Err(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
