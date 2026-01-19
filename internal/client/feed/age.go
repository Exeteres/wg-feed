package feed

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/exeteres/wg-feed/internal/model"
)

func ageIdentityFromURL(raw string) (*age.X25519Identity, bool, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, false, fmt.Errorf("parse url: %w", err)
	}
	frag := strings.TrimSpace(u.Fragment)
	if frag == "" {
		return nil, false, nil
	}
	// Spec: URL fragment MUST be the age secret key with the `AGE-SECRET-KEY-` prefix removed
	// and the remainder lowercased. Reconstruct a parseable identity.
	key := "AGE-SECRET-KEY-" + strings.ToUpper(frag)
	id, err := age.ParseX25519Identity(key)
	if err != nil {
		return nil, true, fmt.Errorf("parse age identity from url fragment: %w", err)
	}
	return id, true, nil
}

func DecryptFeedDocumentForSetupURL(setupURL string, armoredCiphertext string) (model.FeedDocument, error) {
	id, ok, err := ageIdentityFromURL(setupURL)
	if err != nil {
		return model.FeedDocument{}, err
	}
	if !ok {
		return model.FeedDocument{}, &WGFeedError{Status: 200, Message: "encrypted success response but no age key provided in URL fragment", Retriable: false}
	}

	ar := armor.NewReader(strings.NewReader(armoredCiphertext))
	r, err := age.Decrypt(ar, id)
	if err != nil {
		return model.FeedDocument{}, &WGFeedError{Status: 200, Message: "failed to decrypt encrypted_data", Retriable: false}
	}
	pt, err := io.ReadAll(r)
	if err != nil {
		return model.FeedDocument{}, fmt.Errorf("read decrypted feed document: %w", err)
	}

	var doc model.FeedDocument
	if err := json.Unmarshal(pt, &doc); err != nil {
		return model.FeedDocument{}, &WGFeedError{Status: 200, Message: "decrypted feed document is not valid JSON", Retriable: false}
	}
	if err := doc.Validate(); err != nil {
		return model.FeedDocument{}, &WGFeedError{Status: 200, Message: "decrypted feed document failed validation", Retriable: false}
	}
	return doc, nil
}
