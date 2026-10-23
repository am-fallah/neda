package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	sessionTokenBytes = 32
	sessionIDBytes    = 16
	csrfTokenBytes    = 32
	maxUserAgentBytes = 255
)

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func sanitizeUserAgent(raw string) string {
	raw = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, raw))
	if len(raw) > maxUserAgentBytes {
		raw = raw[:maxUserAgentBytes]
		raw = strings.ToValidUTF8(raw, "")
	}
	return raw
}
