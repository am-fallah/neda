// Package push owns VAPID keys, Web Push subscriptions, endpoint validation,
// the send-time SSRF guard, and the server-side Push sender.
package push

import (
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxEndpointBytes bounds a stored Push endpoint. Browsers produce URLs
	// well below this limit; the bound prevents storage abuse.
	MaxEndpointBytes = 2048
	// MaxKeyBytes bounds each subscription key before decoding.
	MaxKeyBytes = 512
	// MaxLabelRunes bounds the owner-supplied device label.
	MaxLabelRunes = 100
)

// ValidationError identifies one invalid subscription field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return "push: validation failed"
	}
	if e.Field == "" {
		return "push: " + e.Message
	}
	return "push: " + e.Field + ": " + e.Message
}

// allowedPushHosts is the code-reviewed exact allowlist of browser Push
// service hosts. A host matches only itself or a subdomain of an entry.
var allowedPushHosts = []string{
	"fcm.googleapis.com",
	"android.googleapis.com",
	"updates.push.services.mozilla.com",
	"push.services.mozilla.com",
	"push.apple.com",
	"notify.windows.com",
}

// ValidateEndpoint applies subscription-time validation rules. The sender
// repeats this validation at send time and also validates resolved/connected
// addresses.
func ValidateEndpoint(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &ValidationError{Field: "endpoint", Message: "is required"}
	}
	if len(raw) > MaxEndpointBytes {
		return &ValidationError{Field: "endpoint", Message: fmt.Sprintf("must be at most %d bytes", MaxEndpointBytes)}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return &ValidationError{Field: "endpoint", Message: "must be a valid absolute URL"}
	}
	if parsed.Scheme != "https" {
		return &ValidationError{Field: "endpoint", Message: "must use https"}
	}
	if parsed.Host == "" {
		return &ValidationError{Field: "endpoint", Message: "must include a host"}
	}
	if parsed.User != nil {
		return &ValidationError{Field: "endpoint", Message: "must not contain userinfo"}
	}
	if parsed.Fragment != "" {
		return &ValidationError{Field: "endpoint", Message: "must not contain a fragment"}
	}
	if parsed.RawQuery != "" && len(parsed.RawQuery) > 1024 {
		return &ValidationError{Field: "endpoint", Message: "query is too long"}
	}
	host := parsed.Hostname()
	if host == "" {
		return &ValidationError{Field: "endpoint", Message: "must include a host"}
	}
	if strings.HasSuffix(host, ".") {
		return &ValidationError{Field: "endpoint", Message: "must not use a trailing dot"}
	}
	if ip := net.ParseIP(host); ip != nil {
		return &ValidationError{Field: "endpoint", Message: "must use a Push service hostname, not an IP address"}
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return &ValidationError{Field: "endpoint", Message: "must use the default HTTPS port"}
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return &ValidationError{Field: "endpoint", Message: "must include a Push service path"}
	}
	if !isAllowedPushHost(host) {
		return &ValidationError{Field: "endpoint", Message: "host is not a recognized Push service"}
	}
	return nil
}

func isAllowedPushHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, allowed := range allowedPushHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// ValidateKeys validates the base64url subscription key material. p256dh must
// be an uncompressed P-256 point and auth must be a 16-byte secret.
func ValidateKeys(p256dh, auth string) error {
	publicKey, err := decodeKey(p256dh)
	if err != nil {
		return &ValidationError{Field: "keys.p256dh", Message: "must be valid base64url"}
	}
	if len(publicKey) != 65 || publicKey[0] != 0x04 {
		return &ValidationError{Field: "keys.p256dh", Message: "must be an uncompressed P-256 public key"}
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), publicKey)
	if x == nil || y == nil {
		return &ValidationError{Field: "keys.p256dh", Message: "must be a valid P-256 public key"}
	}
	authSecret, err := decodeKey(auth)
	if err != nil {
		return &ValidationError{Field: "keys.auth", Message: "must be valid base64url"}
	}
	if len(authSecret) != 16 {
		return &ValidationError{Field: "keys.auth", Message: "must be a 16-byte secret"}
	}
	return nil
}

func decodeKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > MaxKeyBytes {
		return nil, errors.New("push: key length is invalid")
	}
	raw = strings.TrimRight(raw, "=")
	if decoded, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(raw)
}

// ValidateLabel bounds and sanitizes the optional device label.
func ValidateLabel(raw string) (string, error) {
	label := strings.TrimSpace(raw)
	if label == "" {
		return "", nil
	}
	if !utf8.ValidString(label) {
		return "", &ValidationError{Field: "label", Message: "must be valid UTF-8"}
	}
	if utf8.RuneCountInString(label) > MaxLabelRunes {
		return "", &ValidationError{Field: "label", Message: fmt.Sprintf("must be at most %d characters", MaxLabelRunes)}
	}
	for _, runeValue := range label {
		if unicode.IsControl(runeValue) {
			return "", &ValidationError{Field: "label", Message: "must not contain control characters"}
		}
	}
	return label, nil
}

var blockedIPv4 = mustCIDRs(
	"0.0.0.0/8",
	"100.64.0.0/10",    // shared address space (including some metadata services)
	"168.63.129.16/32", // Azure host metadata
	"169.254.0.0/16",   // link-local and cloud metadata
	"192.0.0.0/24",
	"192.0.2.0/24", // documentation
	"198.18.0.0/15",
	"198.51.100.0/24", // documentation
	"203.0.113.0/24",  // documentation
	"240.0.0.0/4",
)

var blockedIPv6 = mustCIDRs(
	"2001:db8::/32", // documentation
	"64:ff9b::/96",  // NAT64 translation of untrusted IPv4
)

func mustCIDRs(values ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic("push: invalid blocked CIDR " + value)
		}
		nets = append(nets, network)
	}
	return nets
}

// IsBlockedIP reports whether an address is unsafe for a Push destination.
// The sender applies this both to every resolved address and to the address
// actually connected.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
		if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsMulticast() {
			return true
		}
		for _, network := range blockedIPv4 {
			if network.Contains(ip) {
				return true
			}
		}
		return false
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() {
		return true
	}
	if !ip.IsGlobalUnicast() {
		return true
	}
	for _, network := range blockedIPv6 {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
