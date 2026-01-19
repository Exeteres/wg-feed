package model

type SuccessResponse struct {
	Version     string `json:"version"`
	Success     bool   `json:"success"`
	Revision    string `json:"revision"`
	TTLSeconds  int    `json:"ttl_seconds"`
	SupportsSSE bool   `json:"supports_sse,omitempty"`

	Encrypted     bool          `json:"encrypted,omitempty"`
	EncryptedData string        `json:"encrypted_data,omitempty"`
	Data          *FeedDocument `json:"data,omitempty"`
}

// FeedEntry is the etcd-stored value for a feed under wg-feed/feeds/<feedPath>.
// It is intentionally not the same as the HTTP response envelope.
//
// Exactly one of the following shapes is allowed:
// - {"encrypted": true,  "encrypted_data": <string>}
// - {"encrypted": false, "data": <FeedDocument>}
type FeedEntry struct {
	Revision   string `json:"revision"`
	TTLSeconds int    `json:"ttl_seconds"`

	Encrypted     bool          `json:"encrypted"`
	EncryptedData string        `json:"encrypted_data,omitempty"`
	Data          *FeedDocument `json:"data,omitempty"`
}

type ErrorResponse struct {
	Version   string `json:"version"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Retriable bool   `json:"retriable"`
}

type FeedDocument struct {
	ID          string      `json:"id"`
	Endpoints   []string    `json:"endpoints"`
	Warning     string      `json:"warning_message,omitempty"`
	DisplayInfo DisplayInfo `json:"display_info"`
	Tunnels     []Tunnel    `json:"tunnels"`
}

type DisplayInfo struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	IconURL     string `json:"icon_url,omitempty"`
}

type Tunnel struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	DisplayInfo   DisplayInfo `json:"display_info"`
	Enabled       bool        `json:"enabled,omitempty"`
	Forced        bool        `json:"forced,omitempty"`
	WGQuickConfig string      `json:"wg_quick_config"`
}
