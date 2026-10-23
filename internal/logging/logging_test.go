package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestJSONLoggerRedactsSensitiveFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "debug", "json")
	logger.Info("provider request",
		"token", "super-secret-token",
		"authorization", "Bearer abc.def.ghi",
		"url", "https://user:pass@example.com/path?token=query-secret#fragment",
		"body", `{"password":"body-secret"}`,
		"request_id", "req-123",
	)

	output := buf.String()
	for _, secret := range []string{"super-secret-token", "abc.def.ghi", "user:pass", "query-secret", "body-secret", "fragment"} {
		if strings.Contains(output, secret) {
			t.Fatalf("log output contains %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, "req-123") {
		t.Fatalf("log output lost non-sensitive request_id: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("log output has no redaction marker: %s", output)
	}
}

func TestTextLoggerHonorsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(&buf, "warn", "text")
	logger.Info("hidden")
	logger.Warn("visible")
	output := buf.String()
	if strings.Contains(output, "hidden") {
		t.Fatalf("info log was emitted at warn level: %s", output)
	}
	if !strings.Contains(output, "visible") {
		t.Fatalf("warn log missing: %s", output)
	}
}

func TestRedactString(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Bearer abc123", "Bearer [REDACTED]"},
		{"token=abc123&other=visible", "token=[REDACTED]&other=visible"},
		{"https://example.com/path?token=abc123#frag", "https://example.com/path"},
		{"prefix https://user:pass@example.com/path?x=1 suffix", "prefix https://example.com/path suffix"},
	}
	for _, tc := range cases {
		if got := RedactString(tc.input); got != tc.want {
			t.Fatalf("RedactString(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRedactURLRejectsRelative(t *testing.T) {
	if got := RedactURL("/relative/path?token=x"); got != redacted {
		t.Fatalf("RedactURL(relative) = %q, want %q", got, redacted)
	}
}

func TestRequestIDContext(t *testing.T) {
	id := NewRequestID()
	if len(id) != 32 {
		t.Fatalf("request ID length = %d, want 32", len(id))
	}
	if id == NewRequestID() {
		t.Fatal("request IDs are not unique")
	}
	ctx := WithRequestID(context.Background(), id)
	if got := RequestID(ctx); got != id {
		t.Fatalf("RequestID = %q, want %q", got, id)
	}
	if got := RequestID(context.Background()); got != "" {
		t.Fatalf("RequestID(empty) = %q", got)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"other": slog.LevelInfo,
	}
	for input, want := range cases {
		if got := ParseLevel(input); got != want {
			t.Fatalf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}
