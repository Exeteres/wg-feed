package feed

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/exeteres/wg-feed/internal/model"
)

func TestDecryptFeedDocument_RoundTrip_WithURLFragmentKey(t *testing.T) {
	t.Parallel()

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	fragment := strings.ToLower(strings.TrimPrefix(id.String(), "AGE-SECRET-KEY-"))
	setupURL := "https://example.test/feed#" + fragment

	doc := model.FeedDocument{
		ID: "11111111-1111-4111-8111-111111111111",
		Endpoints: []string{
			"https://example.test/feed",
		},
		DisplayInfo: model.DisplayInfo{
			Title: "Example",
		},
		Tunnels: []model.Tunnel{{
			ID:   "t1",
			Name: "home",
			DisplayInfo: model.DisplayInfo{
				Title: "Home",
			},
			WGQuickConfig: "[Interface]\nPrivateKey = x\nAddress = 10.0.0.2/32\n",
		}},
	}
	pt, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, id.Recipient())
	if err != nil {
		_ = aw.Close()
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := w.Write(pt); err != nil {
		_ = w.Close()
		_ = aw.Close()
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		_ = aw.Close()
		t.Fatalf("Close: %v", err)
	}
	if err := aw.Close(); err != nil {
		t.Fatalf("ArmorClose: %v", err)
	}
	ciphertext := buf.String()
	if strings.TrimSpace(ciphertext) == "" {
		t.Fatalf("expected non-empty ciphertext")
	}

	got, err := DecryptFeedDocumentForSetupURL(setupURL, ciphertext)
	if err != nil {
		t.Fatalf("DecryptFeedDocumentForURL: %v", err)
	}
	if got.ID != doc.ID {
		t.Fatalf("id mismatch: got %q want %q", got.ID, doc.ID)
	}
	if len(got.Tunnels) != 1 || got.Tunnels[0].WGQuickConfig != doc.Tunnels[0].WGQuickConfig {
		t.Fatalf("wg_quick_config mismatch")
	}
}

func TestDecryptFeedDocumentForSetupURL_NoFragment_ReturnsNonRetriableWGFeedError(t *testing.T) {
	t.Parallel()

	// Any non-empty ciphertext is fine; the code path should fail before decrypt.
	_, err := DecryptFeedDocumentForSetupURL("https://example.test/feed", "-----BEGIN AGE ENCRYPTED FILE-----\n...\n-----END AGE ENCRYPTED FILE-----")
	if err == nil {
		t.Fatalf("expected error")
	}
	wf, ok := AsWGFeedError(err)
	if !ok {
		t.Fatalf("expected WGFeedError, got %T", err)
	}
	if wf.Retriable {
		t.Fatalf("expected non-retriable error")
	}
}
