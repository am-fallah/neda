package delivery_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/delivery"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/store"
)

const (
	telegramTestToken  = "123456:test-token_ABC-def"
	telegramTestChatID = "-1001234567890"
)

func telegramKey(t *testing.T, fill byte) *crypto.Key {
	t.Helper()
	material := make([]byte, crypto.KeySize)
	for i := range material {
		material[i] = fill
	}
	key, err := crypto.NewKey(material)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func telegramSettings(t *testing.T, database *sql.DB, key *crypto.Key, save bool) *telegramsettings.Settings {
	t.Helper()
	st := store.New(database)
	settings, err := telegramsettings.New(st, key, func() time.Time { return workerNow })
	if err != nil {
		t.Fatal(err)
	}
	if save {
		if err := settings.Save(context.Background(), telegramTestToken, telegramTestChatID); err != nil {
			t.Fatal(err)
		}
	}
	return settings
}

func telegramClientFor(t *testing.T, server *httptest.Server) *notify.TelegramClient {
	t.Helper()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := notify.NewTelegramClient(notify.TelegramClientConfig{BaseURL: base, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func telegramWorker(t *testing.T, database *sql.DB, clock *testClock, sender delivery.Sender) *delivery.Worker {
	t.Helper()
	return workerFor(t, database, clock, map[notify.Channel]delivery.Sender{notify.ChannelTelegram: sender}, nil)
}

func TestTelegramSenderDeliversEscapedMessage(t *testing.T) {
	database := newDeliveryDB(t)
	clock := &testClock{now: workerNow}
	var calls atomic.Int32
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.Contains(r.URL.Path, telegramTestToken) {
			t.Errorf("token missing from path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		body = r.PostForm.Get("text")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":1700000000,"chat":{"id":-1001234567890}}}`))
	}))
	defer server.Close()

	settings := telegramSettings(t, database, telegramKey(t, 1), true)
	sender, err := delivery.NewTelegramSender(settings, telegramClientFor(t, server))
	if err != nil {
		t.Fatal(err)
	}
	event := healthEvent("e-escape")
	event.ProviderID = "<script>"
	result := enqueue(t, database, event, notify.Delivery{Channel: notify.ChannelTelegram, TargetKey: "telegram"})
	worker := telegramWorker(t, database, clock, sender)
	if _, err := worker.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, _, _, _, _, _ := deliveryRow(t, database, deliveryID(t, database, result.EventID))
	if status != "sent" || calls.Load() != 1 {
		t.Fatalf("status/calls = %s/%d", status, calls.Load())
	}
	if !strings.Contains(body, "Provider issue: &lt;script&gt;") || strings.Contains(body, "Provider issue: <script>") {
		t.Fatalf("message not escaped:\n%s", body)
	}
}

func TestTelegramSenderAuthorizationAndTransientErrors(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		wantStatus string
		wantKind   string
	}{
		{"unauthorized", http.StatusUnauthorized, "failed", "telegram_permanent"},
		{"forbidden", http.StatusForbidden, "failed", "telegram_permanent"},
		{"server-error", http.StatusInternalServerError, "retry_scheduled", "telegram_transient"},
		{"rate-limited", http.StatusTooManyRequests, "retry_scheduled", "telegram_rate_limited"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			database := newDeliveryDB(t)
			clock := &testClock{now: workerNow}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "1")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"ok":false,"error_code":` + http.StatusText(tc.status) + `}`))
			}))
			defer server.Close()
			settings := telegramSettings(t, database, telegramKey(t, 2), true)
			sender, err := delivery.NewTelegramSender(settings, telegramClientFor(t, server))
			if err != nil {
				t.Fatal(err)
			}
			result := enqueueHealth(t, database, "e-"+tc.name, notify.ChannelTelegram)
			worker := telegramWorker(t, database, clock, sender)
			if _, err := worker.ProcessOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			status, _, _, kind, _, _ := deliveryRow(t, database, deliveryID(t, database, result.EventID))
			if status != tc.wantStatus || kind.String != tc.wantKind {
				t.Fatalf("delivery = %s/%v, want %s/%s", status, kind, tc.wantStatus, tc.wantKind)
			}
		})
	}
}

func TestTelegramSenderSkipsUnconfiguredAndUnreadable(t *testing.T) {
	t.Run("not-configured", func(t *testing.T) {
		database := newDeliveryDB(t)
		clock := &testClock{now: workerNow}
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
		defer server.Close()
		settings := telegramSettings(t, database, telegramKey(t, 3), false)
		sender, err := delivery.NewTelegramSender(settings, telegramClientFor(t, server))
		if err != nil {
			t.Fatal(err)
		}
		result := enqueueHealth(t, database, "e-unconfigured", notify.ChannelTelegram)
		worker := telegramWorker(t, database, clock, sender)
		if _, err := worker.ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		status, _, _, kind, _, _ := deliveryRow(t, database, deliveryID(t, database, result.EventID))
		if status != "skipped" || kind.String != "telegram_not_configured" || calls.Load() != 0 {
			t.Fatalf("delivery = %s/%v calls=%d", status, kind, calls.Load())
		}
	})

	t.Run("wrong-key", func(t *testing.T) {
		database := newDeliveryDB(t)
		clock := &testClock{now: workerNow}
		telegramSettings(t, database, telegramKey(t, 4), true)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer server.Close()
		unreadable := telegramSettings(t, database, telegramKey(t, 5), false)
		sender, err := delivery.NewTelegramSender(unreadable, telegramClientFor(t, server))
		if err != nil {
			t.Fatal(err)
		}
		result := enqueueHealth(t, database, "e-unreadable", notify.ChannelTelegram)
		worker := telegramWorker(t, database, clock, sender)
		if _, err := worker.ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		status, _, _, kind, _, _ := deliveryRow(t, database, deliveryID(t, database, result.EventID))
		if status != "skipped" || kind.String != "telegram_unreadable" {
			t.Fatalf("delivery = %s/%v", status, kind)
		}
	})
}
