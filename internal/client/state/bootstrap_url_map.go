package state

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// CanonicalSetupURLNoFragment returns a stable string form of a Setup URL with its fragment removed.
//
// This is used only for deriving a hashed key for SetupURLMap so the client can correlate
// SETUP_URLS (setup URLs) to feed IDs without persisting the setup URL itself.
func CanonicalSetupURLNoFragment(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	// Drop fragment (age key lives here and must not be part of the stored derivation input).
	u.Fragment = ""
	u.RawFragment = ""

	// Normalize scheme/host casing for stability.
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if host != "" {
		if port := u.Port(); port != "" {
			u.Host = host + ":" + port
		} else {
			u.Host = host
		}
	}

	return u.String(), nil
}

func (st *State) ensureSetupURLSalt() ([]byte, error) {
	if strings.TrimSpace(st.SetupURLSalt) == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		st.SetupURLSalt = hex.EncodeToString(b)
		return b, nil
	}

	b, err := hex.DecodeString(strings.TrimSpace(st.SetupURLSalt))
	if err != nil {
		return nil, fmt.Errorf("invalid setup_url_salt: %w", err)
	}
	if len(b) < 16 {
		return nil, fmt.Errorf("invalid setup_url_salt: too short")
	}
	return b, nil
}

// SetupURLKey returns the stable, salted hash key for SetupURLMap.
func (st *State) SetupURLKey(setupURL string) (string, error) {
	salt, err := st.ensureSetupURLSalt()
	if err != nil {
		return "", err
	}
	canon, err := CanonicalSetupURLNoFragment(setupURL)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(canon))
	sum := mac.Sum(nil)
	return hex.EncodeToString(sum), nil
}
