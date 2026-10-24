package notify_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/notify"
)

const (
	testBotToken = "123456:test-token_ABC-def"
	testChatID   = "-1001234567890"
)

func testTelegramCredentials(t *testing.T) notify.TelegramCredentials {
	t.Helper()
	credentials, err := notify.NewTelegramCredentials(testBotToken, testChatID)
	if err != nil {
		t.Fatalf("NewTelegramCredentials: %v", err)
	}
	return credentials
}

func testTelegramClient(t *testing.T, server *httptest.Server, timeout time.Duration) *notify.TelegramClient {
	t.Helper()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := notify.NewTelegramClient(notify.TelegramClientConfig{
		BaseURL:    base,
		HTTPClient: server.Client(),
		Timeout:    timeout,
	})
	if err != nil {
		t.Fatalf("NewTelegramClient: %v", err)
	}
	return client
}

func TestTelegramCredentialsAreRedacted(t *testing.T) {
	credentials := testTelegramCredentials(t)
	if label := credentials.ChatLabel(); strings.Contains(label, testChatID) || !strings.Contains(label, "7890") {
		t.Fatalf("ChatLabel = %q, want masked last digits", label)
	}

	rendered := fmt.Sprintf("%v %+v %#v %s %x", credentials, credentials, credentials, credentials, credentials)
	if strings.Contains(rendered, testBotToken) || strings.Contains(rendered, testChatID) {
		t.Fatalf("credentials formatting leaked secrets: %q", rendered)
	}
	encoded, err := json.Marshal(credentials)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), testBotToken) || strings.Contains(string(encoded), testChatID) {
		t.Fatalf("credentials JSON leaked secrets: %s", encoded)
	}
}

func TestTelegramCredentialsValidation(t *testing.T) {
	cases := []struct {
		name   string
		token  string
		chatID string
	}{
		{"empty-token", "", testChatID},
		{"token-no-colon", "token", testChatID},
		{"token-whitespace", "123:bad token", testChatID},
		{"token-slash", "123:bad/token", testChatID},
		{"empty-chat", testBotToken, ""},
		{"zero-chat", testBotToken, "0"},
		{"bad-username", testBotToken, "@bad-name"},
		{"short-username", testBotToken, "@abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := notify.NewTelegramCredentials(tc.token, tc.chatID); err == nil {
				t.Fatal("credentials accepted")
			}
		})
	}
	if _, err := notify.NewTelegramCredentials("123:token", "@neda_bot"); err != nil {
		t.Fatalf("valid username credentials rejected: %v", err)
	}
}

func TestTelegramClientGetMe(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/bot"+testBotToken+"/getMe" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":42,"is_bot":true,"username":"neda_bot","first_name":"Neda"}}`))
	}))
	defer server.Close()

	bot, err := testTelegramClient(t, server, time.Second).GetMe(context.Background(), testTelegramCredentials(t))
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if bot.ID != 42 || bot.Username != "neda_bot" || bot.FirstName != "Neda" {
		t.Fatalf("bot = %+v", bot)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestTelegramClientSendMessage(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/bot"+testBotToken+"/sendMessage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9,"date":1700000000,"chat":{"id":-1001234567890}}}`))
	}))
	defer server.Close()

	text := "Yazd &rarr; Mashhad <" + notify.EscapeTelegramHTML("a<b&c>") + ">"
	message, err := testTelegramClient(t, server, time.Second).SendMessage(context.Background(), testTelegramCredentials(t), text)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if message.MessageID != 9 || message.ChatID != -1001234567890 {
		t.Fatalf("message = %+v", message)
	}
	if gotForm.Get("chat_id") != testChatID {
		t.Fatalf("chat_id = %q", gotForm.Get("chat_id"))
	}
	if gotForm.Get("parse_mode") != "HTML" {
		t.Fatalf("parse_mode = %q", gotForm.Get("parse_mode"))
	}
	if gotForm.Get("disable_web_page_preview") != "true" {
		t.Fatalf("disable_web_page_preview = %q", gotForm.Get("disable_web_page_preview"))
	}
	if gotForm.Get("text") != text {
		t.Fatalf("text = %q, want %q", gotForm.Get("text"), text)
	}
}

func TestEscapeTelegramHTML(t *testing.T) {
	got := notify.EscapeTelegramHTML(`<b>&"'`)
	want := "&lt;b&gt;&amp;&#34;&#39;"
	if got != want {
		t.Fatalf("EscapeTelegramHTML = %q, want %q", got, want)
	}
}

func TestTelegramClientClassifiesErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		headers    map[string]string
		wantKind   notify.TelegramErrorKind
		wantRetry  bool
		retryAfter time.Duration
	}{
		{"bad-request", 400, `{"ok":false,"error_code":400,"description":"bad"}`, nil, notify.TelegramErrorPermanent, false, 0},
		{"unauthorized", 401, `{"ok":false,"error_code":401,"description":"bad token"}`, nil, notify.TelegramErrorPermanent, false, 0},
		{"forbidden", 403, `{"ok":false,"error_code":403,"description":"forbidden"}`, nil, notify.TelegramErrorPermanent, false, 0},
		{"rate-limited", 429, `{"ok":false,"error_code":429,"description":"slow down"}`, map[string]string{"Retry-After": "2"}, notify.TelegramErrorRateLimited, true, 2 * time.Second},
		{"server-error", 500, `not json`, nil, notify.TelegramErrorTransient, true, 0},
		{"bad-gateway", 502, ``, nil, notify.TelegramErrorTransient, true, 0},
		{"api-error-in-200", 200, `{"ok":false,"error_code":400,"description":"bad"}`, nil, notify.TelegramErrorPermanent, false, 0},
		{"malformed-json", 200, `{`, nil, notify.TelegramErrorPermanent, false, 0},
		{"missing-ok", 200, `{"result":{}}`, nil, notify.TelegramErrorPermanent, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, value := range tc.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			_, err := testTelegramClient(t, server, time.Second).SendMessage(context.Background(), testTelegramCredentials(t), "hello")
			var telegramErr *notify.TelegramError
			if !errors.As(err, &telegramErr) {
				t.Fatalf("error = %v, want *TelegramError", err)
			}
			if telegramErr.Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", telegramErr.Kind, tc.wantKind)
			}
			if telegramErr.Retryable() != tc.wantRetry {
				t.Fatalf("Retryable = %v, want %v", telegramErr.Retryable(), tc.wantRetry)
			}
			if telegramErr.RetryAfter != tc.retryAfter {
				t.Fatalf("RetryAfter = %v, want %v", telegramErr.RetryAfter, tc.retryAfter)
			}
		})
	}
}

func TestTelegramClientRejectsRedirects(t *testing.T) {
	var nextCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			nextCalls++
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true}}`))
			return
		}
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer server.Close()

	_, err := testTelegramClient(t, server, time.Second).GetMe(context.Background(), testTelegramCredentials(t))
	var telegramErr *notify.TelegramError
	if !errors.As(err, &telegramErr) || telegramErr.Kind != notify.TelegramErrorPermanent {
		t.Fatalf("error = %v, want permanent TelegramError", err)
	}
	if nextCalls != 0 {
		t.Fatalf("redirect target called %d times", nextCalls)
	}
}

func TestTelegramClientTimeoutsAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true}}`))
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	_, err := testTelegramClient(t, server, 20*time.Millisecond).GetMe(context.Background(), testTelegramCredentials(t))
	var telegramErr *notify.TelegramError
	if !errors.As(err, &telegramErr) || telegramErr.Kind != notify.TelegramErrorTransient || !telegramErr.Retryable() {
		t.Fatalf("timeout error = %v, want transient", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = testTelegramClient(t, server, time.Second).GetMe(ctx, testTelegramCredentials(t))
	if !errors.As(err, &telegramErr) || telegramErr.Kind != notify.TelegramErrorCanceled || telegramErr.Retryable() {
		t.Fatalf("cancellation error = %v, want canceled", err)
	}
}

func TestTelegramClientOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":"` + strings.Repeat("x", 100) + `"}`))
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := notify.NewTelegramClient(notify.TelegramClientConfig{
		BaseURL:          base,
		HTTPClient:       server.Client(),
		Timeout:          time.Second,
		MaxResponseBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetMe(context.Background(), testTelegramCredentials(t))
	var telegramErr *notify.TelegramError
	if !errors.As(err, &telegramErr) || telegramErr.Kind != notify.TelegramErrorPermanent {
		t.Fatalf("error = %v, want permanent oversized-response error", err)
	}
}

func TestTelegramClientErrorsDoNotLeakSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"token ` + testBotToken + ` chat ` + testChatID + `"}`))
	}))
	defer server.Close()

	_, err := testTelegramClient(t, server, time.Second).SendMessage(context.Background(), testTelegramCredentials(t), "hello")
	if err == nil {
		t.Fatal("expected error")
	}
	rendered := fmt.Sprintf("%v %+v %#v", err, err, err)
	for _, secret := range []string{testBotToken, testChatID, server.URL, "description"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("error leaked %q: %q", secret, rendered)
		}
	}
}

func TestTelegramClientValidation(t *testing.T) {
	loopback, _ := url.Parse("http://127.0.0.1:1234")
	if _, err := notify.NewTelegramClient(notify.TelegramClientConfig{BaseURL: loopback, Timeout: -time.Second}); err == nil {
		t.Fatal("negative timeout accepted")
	}
	if _, err := notify.NewTelegramClient(notify.TelegramClientConfig{BaseURL: loopback, Timeout: 2 * time.Minute}); err == nil {
		t.Fatal("oversized timeout accepted")
	}
	remoteHTTP, _ := url.Parse("http://api.telegram.org")
	if _, err := notify.NewTelegramClient(notify.TelegramClientConfig{BaseURL: remoteHTTP}); err == nil {
		t.Fatal("non-loopback HTTP base accepted")
	}
	withUser, _ := url.Parse("https://user:pass@api.telegram.org")
	if _, err := notify.NewTelegramClient(notify.TelegramClientConfig{BaseURL: withUser}); err == nil {
		t.Fatal("userinfo base accepted")
	}
	withQuery, _ := url.Parse("https://api.telegram.org?x=1")
	if _, err := notify.NewTelegramClient(notify.TelegramClientConfig{BaseURL: withQuery}); err == nil {
		t.Fatal("query base accepted")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	client := testTelegramClient(t, server, time.Second)
	if _, err := client.SendMessage(context.Background(), testTelegramCredentials(t), "   "); err == nil {
		t.Fatal("blank message accepted")
	}
	if _, err := client.SendMessage(context.Background(), testTelegramCredentials(t), strings.Repeat("x", 4097)); err == nil {
		t.Fatal("oversized message accepted")
	}
}
