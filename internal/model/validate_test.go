package model

import "testing"

func TestFeedDocumentValidate(t *testing.T) {
	valid := FeedDocument{
		ID:        "123e4567-e89b-12d3-a456-426614174000",
		Endpoints: []string{"https://example.com/feed"},
		DisplayInfo: DisplayInfo{
			Title: "Example",
		},
		Tunnels: []Tunnel{
			{
				ID:   "t1",
				Name: "Work",
				DisplayInfo: DisplayInfo{
					Title: "Work",
				},
				WGQuickConfig: "[Interface]\nPrivateKey = x\n",
			},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invalid := valid
	invalid.ID = "not-a-uuid"
	if err := invalid.Validate(); err == nil {
		t.Fatalf("expected error")
	}

	invalid = valid
	invalid.Endpoints = []string{"http://example.com"}
	if err := invalid.Validate(); err == nil {
		t.Fatalf("expected error")
	}

	invalid = valid
	invalid.Tunnels[0].Name = "1bad"
	if err := invalid.Validate(); err == nil {
		t.Fatalf("expected error")
	}
}
