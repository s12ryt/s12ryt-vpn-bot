package domain

import "time"

// TLSSettingsOverview exposes the persisted ACME/TLS configuration to owners.
// The DuckDNS token is never included; only whether one is stored.
type TLSSettingsOverview struct {
	Configured           bool      `json:"configured"`
	Mode                 string    `json:"mode"`
	Domain               string    `json:"domain"`
	Challenge            string    `json:"challenge"`
	Email                string    `json:"email"`
	CADirectoryURLs      []string  `json:"ca_directory_urls"`
	TermsAccepted        bool      `json:"terms_accepted"`
	HasDuckDNSToken      bool      `json:"has_duckdns_token"`
	State                string    `json:"state"`
	CertificateExpiresAt time.Time `json:"certificate_expires_at"`
	LastIssuedCA         string    `json:"last_issued_ca"`
}

// TLSSettingsUpdate is the owner-supplied TLS configuration. DuckDNSToken is
// write-only: an empty value preserves the previously stored token.
type TLSSettingsUpdate struct {
	Mode            string
	Domain          string
	Challenge       string
	Email           string
	CADirectoryURLs []string
	TermsAccepted   bool
	DuckDNSToken    string
}
