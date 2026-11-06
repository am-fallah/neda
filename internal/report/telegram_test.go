package report_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/report"
)

func TestTelegramSenderSendsSilentReport(t *testing.T) {
	st, _ := newReportStore(t)
	material := make([]byte, crypto.KeySize)
	for i := range material {
		material[i] = 0x33
	}
	key, err := crypto.NewKey(material)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := telegramsettings.New(st, key, func() time.Time { return time.Now().UTC() })
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(context.Background(), "123456:test-token_ABC-def", "-1001234567890"); err != nil {
		t.Fatal(err)
	}

	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		form = r.PostForm
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7,"date":1700000000,"chat":{"id":-100}}}`))
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client, err := notify.NewTelegramClient(notify.TelegramClientConfig{BaseURL: base, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	sender, err := report.NewTelegramSender(settings, client)
	if err != nil {
		t.Fatal(err)
	}
	messageID, err := sender.Send(context.Background(), "<b>Neda daily report</b>\n2026-11-05")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if messageID != 7 {
		t.Fatalf("message id = %d, want 7", messageID)
	}
	if form.Get("disable_notification") != "true" {
		t.Fatalf("disable_notification = %q, want true", form.Get("disable_notification"))
	}
	if !strings.Contains(form.Get("text"), "Neda daily report") {
		t.Fatalf("text = %q", form.Get("text"))
	}
	if form.Get("chat_id") != "-1001234567890" {
		t.Fatalf("chat id = %q", form.Get("chat_id"))
	}
}
