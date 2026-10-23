// Package logging provides structured logging and redaction helpers.
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const redacted = "[REDACTED]"

type requestIDKey struct{}

var (
	bearerPattern = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	secretPattern = regexp.MustCompile(`(?i)\b(token|password|passwd|secret|api[_-]?key|authorization|cookie)=([^&\s]+)`)
	urlPattern    = regexp.MustCompile(`https?://[^\s"']+`)
)

// New returns a logger writing JSON in production and text in development.
func New(level, format string) *slog.Logger {
	return NewWithWriter(os.Stdout, level, format)
}

// NewWithWriter is New with an explicit destination, primarily for tests.
func NewWithWriter(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:       ParseLevel(level),
		ReplaceAttr: RedactAttr,
	}
	if strings.EqualFold(format, "text") {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// ParseLevel maps a validated configuration value to a slog level.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// RedactAttr is a slog ReplaceAttr hook that removes credential-bearing values.
func RedactAttr(_ []string, attr slog.Attr) slog.Attr {
	if isSensitiveKey(attr.Key) {
		return slog.String(attr.Key, redacted)
	}
	if attr.Value.Kind() == slog.KindString {
		return slog.String(attr.Key, RedactString(attr.Value.String()))
	}
	return attr
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	if normalized == "" {
		return false
	}
	for _, word := range []string{
		"authorization", "cookie", "password", "passwd", "secret",
		"token", "api_key", "apikey", "private_key", "encryption_key",
		"subscription", "endpoint", "body", "payload", "query",
	} {
		if strings.Contains(normalized, word) {
			return true
		}
	}
	return false
}

// RedactString removes common credential forms and URL query data.
func RedactString(value string) string {
	value = bearerPattern.ReplaceAllString(value, `${1}`+redacted)
	value = secretPattern.ReplaceAllString(value, `${1}=`+redacted)
	return urlPattern.ReplaceAllStringFunc(value, RedactURL)
}

// RedactURL removes userinfo, query, and fragment from an absolute URL while
// retaining scheme, host, and path for useful diagnostics.
func RedactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return redacted
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// NewRequestID returns a random 128-bit request identifier.
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// WithRequestID stores a request identifier in a context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID reads a request identifier from a context.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
