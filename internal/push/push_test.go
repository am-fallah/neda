package push

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/crypto"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/delivery"
	"github.com/am-fallah/neda/internal/store"
)

var testNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

type testEnv struct {
	path    string
	db      *sql.DB
	store   *store.Store
	key     *crypto.Key
	service *Service
}

func newTestKey(t *testing.T, fill byte) *crypto.Key {
	t.Helper()
	material := bytes.Repeat([]byte{fill}, crypto.KeySize)
	key, err := crypto.NewKey(material)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func openTestEnv(t *testing.T, path string, key *crypto.Key, subject string) *testEnv {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if _, err := db.Migrate(ctx, database.DB, migrations.FS); err != nil {
		_ = database.Close()
		t.Fatalf("db.Migrate: %v", err)
	}
	st := store.New(database.DB)
	if err := st.EnsurePushEnabled(ctx, testNow); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	service, err := New(st, database.DB, key, subject, func() time.Time { return testNow })
	if err != nil {
		_ = database.Close()
		t.Fatalf("push.New: %v", err)
	}
	if err := service.Ensure(ctx); err != nil {
		_ = database.Close()
		t.Fatalf("Ensure: %v", err)
	}
	env := &testEnv{path: path, db: database.DB, store: st, key: key, service: service}
	t.Cleanup(func() { _ = database.Close() })
	return env
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return openTestEnv(t, filepath.Join(t.TempDir(), "push.db"), newTestKey(t, 0x42), "mailto:owner@example.com")
}

func validKeyPair(t *testing.T) (string, string) {
	t.Helper()
	private, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil || len(private) == 0 {
		t.Fatalf("generate P-256: %v", err)
	}
	public := elliptic.Marshal(elliptic.P256(), x, y)
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(public), base64.RawURLEncoding.EncodeToString(auth)
}

func allowLocalEndpoints(env *testEnv) {
	env.service.validateStored = func(string) error { return nil }
}

func validEndpoint(suffix string) string {
	return "https://fcm.googleapis.com/fcm/send/" + suffix
}

func registerTestSubscription(t *testing.T, env *testEnv, endpoint string) Subscription {
	t.Helper()
	p256dh, auth := validKeyPair(t)
	subscription, err := env.service.Register(context.Background(), SubscriptionInput{
		Endpoint: endpoint,
		P256DH:   p256dh,
		Auth:     auth,
		Label:    "Test browser",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return subscription
}

func TestValidateEndpointMatrix(t *testing.T) {
	valid := []string{
		validEndpoint("abc"),
		"https://updates.push.services.mozilla.com/wpush/v2/abc",
		"https://web.push.apple.com/abc",
		"https://wns2-par02p.notify.windows.com/w/?token=abc",
		"https://android.googleapis.com/gcm/send/abc",
	}
	for _, endpoint := range valid {
		if err := ValidateEndpoint(endpoint); err != nil {
			t.Fatalf("valid endpoint %q rejected: %v", endpoint, err)
		}
	}

	invalid := []struct {
		name     string
		endpoint string
	}{
		{"empty", ""},
		{"http", "http://fcm.googleapis.com/fcm/send/abc"},
		{"userinfo", "https://user:pass@fcm.googleapis.com/fcm/send/abc"},
		{"fragment", "https://fcm.googleapis.com/fcm/send/abc#frag"},
		{"ipv4", "https://127.0.0.1/fcm/send/abc"},
		{"ipv6", "https://[::1]/fcm/send/abc"},
		{"metadata", "https://169.254.169.254/latest/meta-data"},
		{"unknown-host", "https://evil.example/fcm/send/abc"},
		{"suffix-confusion", "https://fcm.googleapis.com.evil.example/fcm/send/abc"},
		{"custom-port", "https://fcm.googleapis.com:8443/fcm/send/abc"},
		{"no-path", "https://fcm.googleapis.com/"},
		{"trailing-dot", "https://fcm.googleapis.com./fcm/send/abc"},
		{"too-long", "https://fcm.googleapis.com/" + strings.Repeat("a", MaxEndpointBytes)},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateEndpoint(tc.endpoint); err == nil {
				t.Fatalf("invalid endpoint accepted: %q", tc.endpoint)
			}
		})
	}
}

func TestValidateKeysMatrix(t *testing.T) {
	validP256, validAuth := validKeyPair(t)
	if err := ValidateKeys(validP256, validAuth); err != nil {
		t.Fatalf("valid keys rejected: %v", err)
	}
	invalid := []struct {
		name   string
		p256dh string
		auth   string
	}{
		{"empty", "", validAuth},
		{"bad-base64", "!!!!", validAuth},
		{"short-p256", "AAAA", validAuth},
		{"uncompressed-prefix", base64.RawURLEncoding.EncodeToString(make([]byte, 65)), validAuth},
		{"off-curve", base64.RawURLEncoding.EncodeToString(append([]byte{0x04}, bytes.Repeat([]byte{0x01}, 64)...)), validAuth},
		{"short-auth", validP256, base64.RawURLEncoding.EncodeToString(make([]byte, 15))},
		{"long-auth", validP256, base64.RawURLEncoding.EncodeToString(make([]byte, 17))},
		{"oversized", strings.Repeat("A", MaxKeyBytes+1), validAuth},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateKeys(tc.p256dh, tc.auth); err == nil {
				t.Fatal("invalid keys accepted")
			}
		})
	}
}

func TestValidateLabel(t *testing.T) {
	for _, label := range []string{"", "Laptop", strings.Repeat("a", MaxLabelRunes)} {
		if _, err := ValidateLabel(label); err != nil {
			t.Fatalf("valid label %q rejected: %v", label, err)
		}
	}
	for _, label := range []string{strings.Repeat("a", MaxLabelRunes+1), "bad\x00label", "line\nbreak"} {
		if _, err := ValidateLabel(label); err == nil {
			t.Fatalf("invalid label %q accepted", label)
		}
	}
}

func TestVAPIDKeysPersistEncryptedAndStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vapid.db")
	key := newTestKey(t, 0x42)
	env := openTestEnv(t, path, key, "mailto:owner@example.com")
	ctx := context.Background()
	public, err := env.service.vapid.PublicKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if public == "" {
		t.Fatal("empty VAPID public key")
	}
	privateSetting, err := env.store.GetSetting(ctx, VAPIDPrivateKeySetting)
	if err != nil {
		t.Fatal(err)
	}
	if !privateSetting.IsSecret || privateSetting.KeyVersion == nil || *privateSetting.KeyVersion != 1 {
		t.Fatalf("private setting metadata = %+v", privateSetting)
	}
	if !strings.HasPrefix(privateSetting.Value, "v1:") {
		t.Fatal("private key is not versioned ciphertext")
	}
	if strings.Contains(privateSetting.Value, public) {
		t.Fatal("private setting contains the public key")
	}
	if _, err := key.Decrypt(VAPIDPrivateKeySetting, privateSetting.Value); err != nil {
		t.Fatalf("private key cannot be decrypted: %v", err)
	}
	publicSetting, err := env.store.GetSetting(ctx, VAPIDPublicKeySetting)
	if err != nil {
		t.Fatal(err)
	}
	if publicSetting.IsSecret || publicSetting.Value != public {
		t.Fatalf("public setting = %+v", publicSetting)
	}

	// Restart with the same key: the public key must not change.
	_ = env.db.Close()
	env2 := openTestEnv(t, path, key, "mailto:owner@example.com")
	public2, err := env2.service.vapid.PublicKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if public2 != public {
		t.Fatalf("VAPID public key changed across restart: %q != %q", public2, public)
	}
}

func TestVAPIDWrongKeyAndTamperFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vapid.db")
	env := openTestEnv(t, path, newTestKey(t, 0x42), "mailto:owner@example.com")
	_ = env.db.Close()

	wrong := openTestEnvRaw(t, path, newTestKey(t, 0x43), "mailto:owner@example.com")
	if err := wrong.service.Ensure(context.Background()); err == nil {
		t.Fatal("wrong encryption key accepted")
	}
	_ = wrong.db.Close()

	// Tamper the stored ciphertext and reopen with the correct key.
	env = openTestEnvRaw(t, path, newTestKey(t, 0x42), "mailto:owner@example.com")
	if _, err := env.db.Exec(`UPDATE settings SET value = 'v1:AAAA' WHERE key = ?`, VAPIDPrivateKeySetting); err != nil {
		t.Fatal(err)
	}
	_ = env.db.Close()
	tampered := openTestEnvRaw(t, path, newTestKey(t, 0x42), "mailto:owner@example.com")
	if err := tampered.service.Ensure(context.Background()); err == nil {
		t.Fatal("tampered VAPID key accepted")
	}
}

// openTestEnvRaw opens a database and service without requiring Ensure to
// succeed, so fail-closed tests can assert the error.
func openTestEnvRaw(t *testing.T, path string, key *crypto.Key, subject string) *testEnv {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	st := store.New(database.DB)
	service, err := New(st, database.DB, key, subject, func() time.Time { return testNow })
	if err != nil {
		t.Fatalf("push.New: %v", err)
	}
	return &testEnv{path: path, db: database.DB, store: st, key: key, service: service}
}

func TestSubscriptionLifecycleAndEncryption(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	endpoint := validEndpoint("lifecycle")
	subscription := registerTestSubscription(t, env, endpoint)
	if subscription.ID == "" || subscription.Label != "Test browser" {
		t.Fatalf("subscription = %+v", subscription)
	}
	list, err := env.service.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != subscription.ID {
		t.Fatalf("List = %+v, %v", list, err)
	}

	var endpointEnc, p256dhEnc, authEnc []byte
	if err := env.db.QueryRow(`SELECT endpoint_enc, p256dh_enc, auth_enc FROM push_subscriptions WHERE id = ?`, subscription.ID).
		Scan(&endpointEnc, &p256dhEnc, &authEnc); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string][]byte{"endpoint": endpointEnc, "p256dh": p256dhEnc, "auth": authEnc} {
		if len(value) == 0 || strings.Contains(string(value), endpoint) {
			t.Fatalf("%s was not encrypted at rest", name)
		}
	}

	// Re-registering the same endpoint is idempotent and reuses the row ID.
	again := registerTestSubscription(t, env, endpoint)
	if again.ID != subscription.ID {
		t.Fatalf("duplicate registration created a new ID: %q != %q", again.ID, subscription.ID)
	}
	list, _ = env.service.List(ctx)
	if len(list) != 1 {
		t.Fatalf("duplicate rows: %d", len(list))
	}

	if err := env.service.Disable(ctx, subscription.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	list, _ = env.service.List(ctx)
	if len(list) != 0 {
		t.Fatalf("disabled subscription still listed: %+v", list)
	}
	if _, err := env.service.Load(ctx, subscription.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Load disabled = %v", err)
	}
	// Disable is idempotent; an unknown ID is not found.
	if err := env.service.Disable(ctx, subscription.ID); err != nil {
		t.Fatalf("second Disable: %v", err)
	}
	if err := env.service.Disable(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Disable = %v", err)
	}

	// Re-registration re-enables the same row.
	reEnabled := registerTestSubscription(t, env, endpoint)
	if reEnabled.ID != subscription.ID {
		t.Fatalf("re-enable ID = %q", reEnabled.ID)
	}
	if _, err := env.service.Load(ctx, subscription.ID); err != nil {
		t.Fatalf("Load re-enabled: %v", err)
	}
}

func TestSubscriptionWrongKeyFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subs.db")
	env := openTestEnv(t, path, newTestKey(t, 0x42), "mailto:owner@example.com")
	subscription := registerTestSubscription(t, env, validEndpoint("wrong-key"))
	_ = env.db.Close()

	wrong := openTestEnvRaw(t, path, newTestKey(t, 0x43), "mailto:owner@example.com")
	if _, err := wrong.service.Load(context.Background(), subscription.ID); err == nil {
		t.Fatal("subscription decrypted with the wrong key")
	}
}

func TestDeliveryDestinationsRespectEnabledAndDisabled(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	subscription := registerTestSubscription(t, env, validEndpoint("destinations"))

	tx, err := env.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err := env.service.DeliveryDestinations(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if len(deliveries) != 0 {
		t.Fatalf("push disabled but got %d destinations", len(deliveries))
	}

	if err := env.store.SetPushEnabled(ctx, true, testNow); err != nil {
		t.Fatal(err)
	}
	tx, _ = env.db.BeginTx(ctx, nil)
	deliveries, err = env.service.DeliveryDestinations(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if len(deliveries) != 1 || deliveries[0].Channel != notify.ChannelPush ||
		deliveries[0].TargetKey != "push:"+subscription.ID {
		t.Fatalf("destinations = %+v", deliveries)
	}

	if err := env.service.Disable(ctx, subscription.ID); err != nil {
		t.Fatal(err)
	}
	tx, _ = env.db.BeginTx(ctx, nil)
	deliveries, err = env.service.DeliveryDestinations(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if len(deliveries) != 0 {
		t.Fatalf("disabled subscription produced %d destinations", len(deliveries))
	}
}

func TestSenderRejectsUnavailableAndDisabled(t *testing.T) {
	env := newTestEnv(t)
	sender, err := NewSender(env.service, SenderConfig{Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	subscription := registerTestSubscription(t, env, validEndpoint("sender-gates"))
	record := delivery.Record{
		Channel:   notify.ChannelPush,
		TargetKey: "push:" + subscription.ID,
		Event:     notify.Event{ID: "e1", Kind: notify.KindAvailability, OccurredAt: testNow},
	}
	if result := sender.Send(context.Background(), record); result.Outcome != delivery.OutcomeSkipped || result.ErrorKind != "push_disabled" {
		t.Fatalf("disabled send = %+v", result)
	}
	if err := env.store.SetPushEnabled(context.Background(), true, testNow); err != nil {
		t.Fatal(err)
	}
	if err := env.service.Disable(context.Background(), subscription.ID); err != nil {
		t.Fatal(err)
	}
	if result := sender.Send(context.Background(), record); result.Outcome != delivery.OutcomeSkipped || result.ErrorKind != "push_subscription_disabled" {
		t.Fatalf("disabled subscription send = %+v", result)
	}
	if _, err := sender.Test(context.Background(), ""); !errors.Is(err, ErrNoSubscriptions) {
		t.Fatalf("empty Test = %v", err)
	}
}

func TestSenderTestRequiresEnabled(t *testing.T) {
	env := newTestEnv(t)
	sender, _ := NewSender(env.service, SenderConfig{Now: func() time.Time { return testNow }})
	if _, err := sender.Test(context.Background(), ""); !errors.Is(err, ErrPushDisabled) {
		t.Fatalf("test while disabled = %v", err)
	}
}

func TestSenderBlockedResolution(t *testing.T) {
	env := newTestEnv(t)
	if err := env.store.SetPushEnabled(context.Background(), true, testNow); err != nil {
		t.Fatal(err)
	}
	endpoint := validEndpoint("blocked")
	p256dh, auth := validKeyPair(t)
	subscription, err := env.service.storeSubscription(context.Background(), endpoint, p256dh, auth, "")
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{answers: map[string][]net.IPAddr{
		"fcm.googleapis.com": {{IP: net.ParseIP("10.0.0.8")}},
	}}
	sender, err := NewSender(env.service, SenderConfig{
		Resolver: resolver,
		Now:      func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	result := sender.Send(context.Background(), delivery.Record{
		Channel:   notify.ChannelPush,
		TargetKey: "push:" + subscription.ID,
		Event:     notify.Event{ID: "e1", Kind: notify.KindAvailability, OccurredAt: testNow},
	})
	if result.Outcome != delivery.OutcomeFailed || result.ErrorKind != "push_blocked_address" {
		t.Fatalf("blocked send = %+v", result)
	}
}

type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]net.IPAddr
	err     error
	calls   int
}

func (f *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.answers[host], nil
}

func TestGuardedDialerResolvesOnceAndValidatesConnectedAddress(t *testing.T) {
	public := net.ParseIP("93.184.216.34")
	private := net.ParseIP("10.0.0.9")
	resolver := &sequenceResolver{answers: []net.IPAddr{{IP: public}, {IP: private}}}
	var dialed string
	dialer := &guardedDialer{
		resolver: resolver,
		timeout:  time.Second,
		dial: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialed = address
			client, server := net.Pipe()
			_ = server.Close()
			return &remoteConn{Conn: client, remote: &net.TCPAddr{IP: public, Port: 443}}, nil
		},
	}
	conn, err := dialer.DialContext(context.Background(), "tcp", "fcm.googleapis.com:443")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	_ = conn.Close()
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}
	if dialed != "93.184.216.34:443" {
		t.Fatalf("dialed %q", dialed)
	}

	// A later private DNS answer must not matter because it is never queried,
	// and a connected private IP must still be rejected.
	connectedPrivate := &guardedDialer{
		resolver: &fakeResolver{answers: map[string][]net.IPAddr{
			"fcm.googleapis.com": {{IP: public}},
		}},
		timeout: time.Second,
		dial: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			_ = server.Close()
			return &remoteConn{Conn: client, remote: &net.TCPAddr{IP: private, Port: 443}}, nil
		},
	}
	if _, err := connectedPrivate.DialContext(context.Background(), "tcp", "fcm.googleapis.com:443"); err == nil {
		t.Fatal("connected private address accepted")
	}

	// Any private answer in the DNS set fails closed, even if another answer is
	// public.
	mixed := &fakeResolver{answers: map[string][]net.IPAddr{
		"fcm.googleapis.com": {{IP: public}, {IP: private}},
	}}
	dialer = &guardedDialer{resolver: mixed, timeout: time.Second, dial: func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dial must not be called for a mixed DNS answer")
		return nil, nil
	}}
	if _, err := dialer.DialContext(context.Background(), "tcp", "fcm.googleapis.com:443"); err == nil {
		t.Fatal("mixed public/private DNS answer accepted")
	}
}

type sequenceResolver struct {
	mu      sync.Mutex
	answers []net.IPAddr
	calls   int
}

func (s *sequenceResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.answers) == 0 {
		return nil, errors.New("no answers")
	}
	answer := s.answers[0]
	if len(s.answers) > 1 {
		s.answers = s.answers[1:]
	}
	return []net.IPAddr{answer}, nil
}

type remoteConn struct {
	net.Conn
	remote net.Addr
}

func (c *remoteConn) RemoteAddr() net.Addr { return c.remote }

func TestIsBlockedIPMatrix(t *testing.T) {
	blocked := []string{
		"0.0.0.0", "127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", "100.100.100.200", "168.63.129.16", "100.64.0.1",
		"192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1",
		"224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::1", "::", "fc00::1", "fd00:ec2::254", "fe80::1", "ff02::1",
		"2001:db8::1", "64:ff9b::1",
	}
	for _, raw := range blocked {
		if !IsBlockedIP(net.ParseIP(raw)) {
			t.Fatalf("%s was allowed", raw)
		}
	}
	for _, raw := range []string{"93.184.216.34", "142.250.185.78", "2606:4700:4700::1111"} {
		if IsBlockedIP(net.ParseIP(raw)) {
			t.Fatalf("%s was blocked", raw)
		}
	}
	if !IsBlockedIP(nil) {
		t.Fatal("nil IP was allowed")
	}
}

func TestPushSendStatusHandling(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		header     map[string]string
		want       delivery.Outcome
		kind       string
		retryAfter time.Duration
	}{
		{"created", http.StatusCreated, nil, delivery.OutcomeSent, "", 0},
		{"no-content", http.StatusNoContent, nil, delivery.OutcomeSent, "", 0},
		{"gone", http.StatusGone, nil, delivery.OutcomeFailed, "push_gone", 0},
		{"rate-limited", http.StatusTooManyRequests, map[string]string{"Retry-After": "7"}, delivery.OutcomeRetry, "push_rate_limited", 7 * time.Second},
		{"server-error", http.StatusInternalServerError, nil, delivery.OutcomeRetry, "push_transient", 0},
		{"bad-request", http.StatusBadRequest, nil, delivery.OutcomeFailed, "push_permanent", 0},
		{"forbidden", http.StatusForbidden, nil, delivery.OutcomeFailed, "push_permanent", 0},
		{"teapot", http.StatusTeapot, nil, delivery.OutcomeFailed, "push_rejected", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			allowLocalEndpoints(env)
			if err := env.store.SetPushEnabled(context.Background(), true, testNow); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s", r.Method)
				}
				if r.Header.Get("TTL") == "" || r.Header.Get("Urgency") != "high" {
					t.Errorf("missing push headers: %v", r.Header)
				}
				auth := r.Header.Get("Authorization")
				if !strings.HasPrefix(auth, "vapid t=") || strings.Contains(auth, "vapid t=, ") {
					t.Errorf("authorization = %q", auth)
				}
				for key, value := range tc.header {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			p256dh, auth := validKeyPair(t)
			subscription, err := env.service.storeSubscription(context.Background(), server.URL+"/push/token", p256dh, auth, "")
			if err != nil {
				t.Fatal(err)
			}
			client := testHTTPClient(t, server)
			sender, err := NewSender(env.service, SenderConfig{HTTPClient: client, Now: func() time.Time { return testNow }})
			if err != nil {
				t.Fatal(err)
			}
			sender.validate = func(string) error { return nil }
			result := sender.Send(context.Background(), delivery.Record{
				Channel:   notify.ChannelPush,
				TargetKey: "push:" + subscription.ID,
				Event:     notify.Event{ID: "e1", Kind: notify.KindAvailability, OccurredAt: testNow},
			})
			if result.Outcome != tc.want || result.ErrorKind != tc.kind {
				t.Fatalf("result = %+v, want %s/%s", result, tc.want, tc.kind)
			}
			if tc.retryAfter > 0 && result.RetryAfter != tc.retryAfter {
				t.Fatalf("retryAfter = %s, want %s", result.RetryAfter, tc.retryAfter)
			}
			switch tc.status {
			case http.StatusGone:
				if _, err := env.service.Load(context.Background(), subscription.ID); !errors.Is(err, ErrDisabled) {
					t.Fatalf("gone subscription not disabled: %v", err)
				}
			case http.StatusCreated, http.StatusNoContent:
				var lastSuccess sql.NullString
				if err := env.db.QueryRow(`SELECT last_success_at FROM push_subscriptions WHERE id = ?`, subscription.ID).Scan(&lastSuccess); err != nil {
					t.Fatal(err)
				}
				if !lastSuccess.Valid {
					t.Fatal("success was not recorded")
				}
			}
		})
	}
}

func testHTTPClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	client := server.Client()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("test client has no *http.Transport")
	}
	dialer := newGuardedDialer(net.DefaultResolver, time.Second)
	dialer.allowTests = true
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errPushRedirect }
	return client
}

func TestSenderRedirectRejected(t *testing.T) {
	env := newTestEnv(t)
	allowLocalEndpoints(env)
	if err := env.store.SetPushEnabled(context.Background(), true, testNow); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://127.0.0.1/private", http.StatusFound)
	}))
	defer server.Close()
	p256dh, auth := validKeyPair(t)
	subscription, _ := env.service.storeSubscription(context.Background(), server.URL+"/push", p256dh, auth, "")
	sender, _ := NewSender(env.service, SenderConfig{HTTPClient: testHTTPClient(t, server), Now: func() time.Time { return testNow }})
	sender.validate = func(string) error { return nil }
	result := sender.Send(context.Background(), delivery.Record{
		Channel:   notify.ChannelPush,
		TargetKey: "push:" + subscription.ID,
		Event:     notify.Event{ID: "e1", Kind: notify.KindAvailability, OccurredAt: testNow},
	})
	if result.Outcome != delivery.OutcomeFailed || result.ErrorKind != "push_redirect_rejected" {
		t.Fatalf("redirect result = %+v", result)
	}
}

func TestSenderTestSendsToActiveDevice(t *testing.T) {
	env := newTestEnv(t)
	allowLocalEndpoints(env)
	if err := env.store.SetPushEnabled(context.Background(), true, testNow); err != nil {
		t.Fatal(err)
	}
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Error("empty push body")
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	p256dh, auth := validKeyPair(t)
	subscription, _ := env.service.storeSubscription(context.Background(), server.URL+"/push", p256dh, auth, "Laptop")
	sender, _ := NewSender(env.service, SenderConfig{HTTPClient: testHTTPClient(t, server), Now: func() time.Time { return testNow }})
	sender.validate = func(string) error { return nil }
	outcome, err := sender.Test(context.Background(), subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || outcome.Attempted != 1 || outcome.Sent != 1 || outcome.Failed != 0 {
		t.Fatalf("calls=%d outcome=%+v", calls, outcome)
	}
}
