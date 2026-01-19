package model

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	uuidRe       = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	tunnelNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
)

func (r SuccessResponse) Validate() error {
	if r.Version != "wg-feed-00" {
		return fmt.Errorf("version must be wg-feed-00")
	}
	if !r.Success {
		return fmt.Errorf("success must be true")
	}
	if strings.TrimSpace(r.Revision) == "" {
		return fmt.Errorf("revision is required")
	}
	if r.TTLSeconds < 0 {
		return fmt.Errorf("ttl_seconds must be >= 0")
	}
	if r.Encrypted {
		if strings.TrimSpace(r.EncryptedData) == "" {
			return fmt.Errorf("encrypted_data is required when encrypted=true")
		}
		if r.Data != nil {
			return fmt.Errorf("data must be omitted when encrypted=true")
		}
		return nil
	}
	if strings.TrimSpace(r.EncryptedData) != "" {
		return fmt.Errorf("encrypted_data must be omitted when encrypted=false")
	}
	if r.Data == nil {
		return fmt.Errorf("data is required when encrypted=false")
	}
	if err := r.Data.Validate(); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return nil
}

func (e FeedEntry) Validate() error {
	if strings.TrimSpace(e.Revision) == "" {
		return fmt.Errorf("revision is required")
	}
	if e.TTLSeconds < 0 {
		return fmt.Errorf("ttl_seconds must be >= 0")
	}
	if e.Encrypted {
		if strings.TrimSpace(e.EncryptedData) == "" {
			return fmt.Errorf("encrypted_data is required when encrypted=true")
		}
		if e.Data != nil {
			return fmt.Errorf("data must be omitted when encrypted=true")
		}
		return nil
	}
	if strings.TrimSpace(e.EncryptedData) != "" {
		return fmt.Errorf("encrypted_data must be omitted when encrypted=false")
	}
	if e.Data == nil {
		return fmt.Errorf("data is required when encrypted=false")
	}
	if err := e.Data.Validate(); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return nil
}

func (r ErrorResponse) Validate() error {
	if r.Version != "wg-feed-00" {
		return fmt.Errorf("version must be wg-feed-00")
	}
	if r.Success {
		return fmt.Errorf("success must be false")
	}
	if strings.TrimSpace(r.Message) == "" {
		return fmt.Errorf("message is required")
	}
	return nil
}

func (f FeedDocument) Validate() error {
	if !uuidRe.MatchString(f.ID) {
		return fmt.Errorf("id must be a UUID")
	}
	if len(f.Endpoints) == 0 {
		return fmt.Errorf("endpoints must contain at least one item")
	}
	if f.Warning != "" && strings.TrimSpace(f.Warning) == "" {
		return fmt.Errorf("warning_message must be non-empty when present")
	}
	for i, raw := range f.Endpoints {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("endpoints[%d]: invalid url", i)
		}
		if u.Scheme != "https" {
			return fmt.Errorf("endpoints[%d]: scheme must be https", i)
		}
		if strings.TrimSpace(u.Host) == "" {
			return fmt.Errorf("endpoints[%d]: host is required", i)
		}
		if strings.TrimSpace(u.Fragment) != "" {
			return fmt.Errorf("endpoints[%d]: fragment must be omitted", i)
		}
	}
	if strings.TrimSpace(f.DisplayInfo.Title) == "" {
		return fmt.Errorf("display_info.title is required")
	}
	if f.DisplayInfo.IconURL != "" {
		if err := validateIconURL(f.DisplayInfo.IconURL); err != nil {
			return fmt.Errorf("display_info.icon_url: %w", err)
		}
	}
	if f.Tunnels == nil {
		return fmt.Errorf("tunnels is required")
	}

	seenTunnelIDs := make(map[string]struct{}, len(f.Tunnels))
	for i, t := range f.Tunnels {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("tunnels[%d]: %w", i, err)
		}
		if _, ok := seenTunnelIDs[t.ID]; ok {
			return fmt.Errorf("tunnels[%d].id duplicates another tunnel id", i)
		}
		seenTunnelIDs[t.ID] = struct{}{}
	}
	return nil
}

func (t Tunnel) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !tunnelNameRe.MatchString(t.Name) {
		return fmt.Errorf("name must match %s", tunnelNameRe.String())
	}
	if strings.TrimSpace(t.DisplayInfo.Title) == "" {
		return fmt.Errorf("display_info.title is required")
	}
	if t.DisplayInfo.IconURL != "" {
		if err := validateIconURL(t.DisplayInfo.IconURL); err != nil {
			return fmt.Errorf("display_info.icon_url: %w", err)
		}
	}
	if strings.TrimSpace(t.WGQuickConfig) == "" {
		return fmt.Errorf("wg_quick_config is required")
	}
	return nil
}

func validateIconURL(raw string) error {
	// Schema and draft require an SVG data: URL (image/svg+xml).
	s := strings.ToLower(strings.TrimSpace(raw))
	if !strings.HasPrefix(s, "data:") {
		return fmt.Errorf("must be a data: URL")
	}
	s = strings.TrimPrefix(s, "data:")
	if !strings.HasPrefix(s, "image/svg+xml") {
		return fmt.Errorf("must be an SVG data: URL (image/svg+xml)")
	}
	// Require a delimiter after the media type: either parameters ';' or the data separator ','.
	if len(s) == len("image/svg+xml") {
		return fmt.Errorf("invalid data: URL")
	}
	next := s[len("image/svg+xml")]
	if next != ';' && next != ',' {
		return fmt.Errorf("invalid data: URL")
	}
	return nil
}
