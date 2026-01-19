package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/exeteres/wg-feed/internal/model"
)

const AgeArmoredPrefix = "-----BEGIN AGE ENCRYPTED FILE-----"

type ParsedInput struct {
	RevisionMaterial []byte
	Data             map[string]any
	EncryptedData    string
	Encrypted        bool
}

func ParseFeedPath(raw string) (string, error) {
	feedPath := strings.TrimSpace(raw)
	feedPath = strings.Trim(feedPath, "/")
	if feedPath == "" {
		return "", errors.New("feedPath must be non-empty")
	}
	return feedPath, nil
}

func ValidateTTLSeconds(ttlSeconds int) error {
	if ttlSeconds < 0 {
		return fmt.Errorf("ttl must be >= 0")
	}
	return nil
}

func ParseInput(input string) (ParsedInput, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ParsedInput{}, errors.New("stdin must be non-empty")
	}

	if strings.HasPrefix(trimmed, AgeArmoredPrefix) {
		return ParsedInput{
			Encrypted:        true,
			EncryptedData:    trimmed,
			RevisionMaterial: []byte(trimmed),
		}, nil
	}

	var v any
	dec := json.NewDecoder(strings.NewReader(trimmed))
	if err := dec.Decode(&v); err != nil {
		return ParsedInput{}, fmt.Errorf("decode feed document JSON: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return ParsedInput{}, errors.New("decode feed document JSON: trailing data")
	}

	m, ok := v.(map[string]any)
	if !ok {
		return ParsedInput{}, errors.New("feed document must be a JSON object")
	}

	b, err := json.Marshal(m)
	if err != nil {
		return ParsedInput{}, fmt.Errorf("canonicalize feed document: %w", err)
	}

	var doc model.FeedDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		return ParsedInput{}, fmt.Errorf("decode feed document: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return ParsedInput{}, fmt.Errorf("validate feed document: %w", err)
	}

	return ParsedInput{
		Encrypted:        false,
		Data:             m,
		RevisionMaterial: b,
	}, nil
}

func ComputeRevision(material []byte) string {
	h := sha256.Sum256(material)
	return hex.EncodeToString(h[:])
}

// BuildStoreBodyJSON returns the etcd value body for a feed, along with its revision.
func BuildStoreBodyJSON(ttlSeconds int, parsed ParsedInput) ([]byte, string, error) {
	if err := ValidateTTLSeconds(ttlSeconds); err != nil {
		return nil, "", err
	}
	if len(parsed.RevisionMaterial) == 0 {
		return nil, "", errors.New("revision material must be non-empty")
	}

	revision := ComputeRevision(parsed.RevisionMaterial)
	entryObj := map[string]any{
		"revision":       revision,
		"ttl_seconds":    ttlSeconds,
		"encrypted":      parsed.Encrypted,
		"data":           nil,
		"encrypted_data": nil,
	}

	if parsed.Encrypted {
		entryObj["encrypted_data"] = parsed.EncryptedData
		delete(entryObj, "data")
	} else {
		entryObj["data"] = parsed.Data
		delete(entryObj, "encrypted_data")
	}

	storeBody, err := json.Marshal(entryObj)
	if err != nil {
		return nil, "", fmt.Errorf("encode feed entry: %w", err)
	}
	return storeBody, revision, nil
}
