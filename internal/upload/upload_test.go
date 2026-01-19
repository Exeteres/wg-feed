package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestParseFeedPath(t *testing.T) {
	got, err := ParseFeedPath(" /foo/bar/ ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "foo/bar" {
		t.Fatalf("unexpected feedPath: %q", got)
	}

	if _, err := ParseFeedPath("   "); err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseInput_Encrypted(t *testing.T) {
	inp := AgeArmoredPrefix + "\n..."
	parsed, err := ParseInput(inp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !parsed.Encrypted {
		t.Fatalf("expected encrypted")
	}
	if parsed.EncryptedData == "" {
		t.Fatalf("expected encrypted_data")
	}
	if string(parsed.RevisionMaterial) != inp {
		t.Fatalf("unexpected revision material")
	}
}

func TestParseInput_JSONValidatesAndCanonicalizes(t *testing.T) {
	// Minimal valid feed document.
	jsonDoc := `{
		"id": "123e4567-e89b-12d3-a456-426614174000",
		"endpoints": ["https://example.com/feed"],
		"display_info": {"title": "Example"},
		"tunnels": [
			{
				"id": "t1",
				"name": "Work",
				"display_info": {"title": "Work"},
				"wg_quick_config": "[Interface]\nPrivateKey = x\n"
			}
		]
	}`

	parsed, err := ParseInput(jsonDoc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Encrypted {
		t.Fatalf("expected unencrypted")
	}
	if parsed.Data == nil {
		t.Fatalf("expected data map")
	}
	if len(parsed.RevisionMaterial) == 0 {
		t.Fatalf("expected revision material")
	}

	h := sha256.Sum256(parsed.RevisionMaterial)
	expectedRevision := hex.EncodeToString(h[:])
	_, gotRevision, err := BuildStoreBodyJSON(60, parsed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRevision != expectedRevision {
		t.Fatalf("revision mismatch: got=%s want=%s", gotRevision, expectedRevision)
	}
}

func TestParseInput_JSONTrailingData(t *testing.T) {
	_, err := ParseInput(`{"id": "123e4567-e89b-12d3-a456-426614174000", "endpoints": ["https://example.com"], "display_info": {"title":"t"}, "tunnels": []} {}`)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildStoreBodyJSON_TTLMustBeNonNegative(t *testing.T) {
	parsed := ParsedInput{Encrypted: true, EncryptedData: AgeArmoredPrefix, RevisionMaterial: []byte(AgeArmoredPrefix)}
	_, _, err := BuildStoreBodyJSON(-1, parsed)
	if err == nil {
		t.Fatalf("expected error")
	}
}
