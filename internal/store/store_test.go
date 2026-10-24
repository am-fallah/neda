package store_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/store"
)

var testNow = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)

func newTestStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	database, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "neda-test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("database.Close: %v", err)
		}
	})

	applied, err := db.Migrate(context.Background(), database.DB, migrations.FS)
	if err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	if err := database.Ready(context.Background(), applied); err != nil {
		t.Fatalf("database.Ready: %v", err)
	}
	return store.New(database.DB), database.DB
}

func hash(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func TestWithTxCommitAndRollback(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	committed := store.Setting{
		Key:       "poll.interval",
		Value:     "300",
		UpdatedAt: testNow,
	}
	if err := st.WithTx(ctx, func(tx *store.Store) error {
		return tx.SetSetting(ctx, committed)
	}); err != nil {
		t.Fatalf("WithTx commit: %v", err)
	}
	if _, err := st.GetSetting(ctx, committed.Key); err != nil {
		t.Fatalf("GetSetting after commit: %v", err)
	}

	rolledBack := store.Setting{
		Key:       "push.enabled",
		Value:     "true",
		UpdatedAt: testNow,
	}
	boom := errors.New("boom")
	err := st.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.SetSetting(ctx, rolledBack); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithTx error = %v, want %v", err, boom)
	}
	if _, err := st.GetSetting(ctx, rolledBack.Key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSetting after rollback error = %v, want ErrNotFound", err)
	}
}

func TestWithTxPanicRollsBack(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	setting := store.Setting{Key: "panic", Value: "value", UpdatedAt: testNow}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = st.WithTx(ctx, func(tx *store.Store) error {
			if err := tx.SetSetting(ctx, setting); err != nil {
				return err
			}
			panic("transaction panic")
		})
	}()
	if recovered == nil {
		t.Fatal("expected panic to propagate")
	}
	if _, err := st.GetSetting(ctx, setting.Key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSetting after panic error = %v, want ErrNotFound", err)
	}
}

func TestWithTxRejectsNestedAndCanceledContext(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	err := st.WithTx(ctx, func(tx *store.Store) error {
		return tx.WithTx(ctx, func(*store.Store) error { return nil })
	})
	if !errors.Is(err, store.ErrNestedTx) {
		t.Fatalf("nested WithTx error = %v, want ErrNestedTx", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := st.WithTx(canceled, func(*store.Store) error { return nil }); err == nil {
		t.Fatal("WithTx with canceled context succeeded")
	}
}

func TestWithSQLTxCommitRollbackAndNested(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	direct := store.Setting{Key: "sql.direct", Value: "one", UpdatedAt: testNow}
	throughStore := store.Setting{Key: "sql.store", Value: "two", UpdatedAt: testNow}
	if err := st.WithSQLTx(ctx, func(tx *sql.Tx, txStore *store.Store) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, is_secret, updated_at) VALUES (?, ?, 0, ?)`,
			direct.Key, direct.Value, direct.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return txStore.SetSetting(ctx, throughStore)
	}); err != nil {
		t.Fatalf("WithSQLTx commit: %v", err)
	}
	if _, err := st.GetSetting(ctx, direct.Key); err != nil {
		t.Fatalf("direct setting after commit: %v", err)
	}
	if _, err := st.GetSetting(ctx, throughStore.Key); err != nil {
		t.Fatalf("store setting after commit: %v", err)
	}

	rollback := store.Setting{Key: "sql.rollback", Value: "three", UpdatedAt: testNow}
	boom := errors.New("boom")
	err := st.WithSQLTx(ctx, func(_ *sql.Tx, txStore *store.Store) error {
		if err := txStore.SetSetting(ctx, rollback); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithSQLTx error = %v, want %v", err, boom)
	}
	if _, err := st.GetSetting(ctx, rollback.Key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rollback setting error = %v, want ErrNotFound", err)
	}

	err = st.WithSQLTx(ctx, func(_ *sql.Tx, txStore *store.Store) error {
		return txStore.WithSQLTx(ctx, func(*sql.Tx, *store.Store) error { return nil })
	})
	if !errors.Is(err, store.ErrNestedTx) {
		t.Fatalf("nested WithSQLTx error = %v, want ErrNestedTx", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := st.WithSQLTx(canceled, func(*sql.Tx, *store.Store) error { return nil }); err == nil {
		t.Fatal("WithSQLTx with canceled context succeeded")
	}
}

func TestSettingsCRUD(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	keyVersion := int64(1)
	setting := store.Setting{
		Key:        "telegram.bot_token",
		Value:      "ciphertext",
		IsSecret:   true,
		KeyVersion: &keyVersion,
		UpdatedAt:  testNow,
	}
	if err := st.SetSetting(ctx, setting); err != nil {
		t.Fatalf("SetSetting insert: %v", err)
	}
	got, err := st.GetSetting(ctx, setting.Key)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if got.Key != setting.Key || got.Value != setting.Value || !got.IsSecret {
		t.Fatalf("GetSetting = %+v, want key/value/secret from %+v", got, setting)
	}
	if got.KeyVersion == nil || *got.KeyVersion != keyVersion {
		t.Fatalf("KeyVersion = %v, want %d", got.KeyVersion, keyVersion)
	}
	if !got.UpdatedAt.Equal(testNow) {
		t.Fatalf("UpdatedAt = %v, want %v", got.UpdatedAt, testNow)
	}

	updated := store.Setting{Key: setting.Key, Value: "plain", UpdatedAt: testNow.Add(time.Minute)}
	if err := st.SetSetting(ctx, updated); err != nil {
		t.Fatalf("SetSetting update: %v", err)
	}
	got, err = st.GetSetting(ctx, setting.Key)
	if err != nil {
		t.Fatalf("GetSetting updated: %v", err)
	}
	if got.Value != "plain" || got.IsSecret || got.KeyVersion != nil {
		t.Fatalf("updated setting = %+v, want plain non-secret without key version", got)
	}

	if err := st.DeleteSetting(ctx, setting.Key); err != nil {
		t.Fatalf("DeleteSetting: %v", err)
	}
	if _, err := st.GetSetting(ctx, setting.Key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSetting deleted error = %v, want ErrNotFound", err)
	}
	if err := st.DeleteSetting(ctx, setting.Key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteSetting missing error = %v, want ErrNotFound", err)
	}
}

func TestSettingsValidation(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	if _, err := st.GetSetting(ctx, " "); err == nil {
		t.Fatal("GetSetting with empty key succeeded")
	}
	if err := st.SetSetting(ctx, store.Setting{Key: "", UpdatedAt: testNow}); err == nil {
		t.Fatal("SetSetting with empty key succeeded")
	}
	if err := st.SetSetting(ctx, store.Setting{Key: "x"}); err == nil {
		t.Fatal("SetSetting with zero timestamp succeeded")
	}
	keyVersion := int64(0)
	if err := st.SetSetting(ctx, store.Setting{Key: "x", KeyVersion: &keyVersion, UpdatedAt: testNow}); err == nil {
		t.Fatal("SetSetting with zero key version succeeded")
	}
}

func TestUsersSingleRowAndPasswordUpdate(t *testing.T) {
	st, sqlDB := newTestStore(t)
	ctx := context.Background()

	if _, err := st.GetUser(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetUser on empty database error = %v, want ErrNotFound", err)
	}

	if err := st.UpsertUser(ctx, "hash-one", testNow); err != nil {
		t.Fatalf("UpsertUser insert: %v", err)
	}
	user, err := st.GetUser(ctx)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.ID != 1 || user.PasswordHash != "hash-one" {
		t.Fatalf("user = %+v, want id=1 hash=hash-one", user)
	}
	if !user.CreatedAt.Equal(testNow) || !user.PasswordChangedAt.Equal(testNow) {
		t.Fatalf("user timestamps = %v/%v, want %v", user.CreatedAt, user.PasswordChangedAt, testNow)
	}

	later := testNow.Add(time.Hour)
	if err := st.UpsertUser(ctx, "hash-two", later); err != nil {
		t.Fatalf("UpsertUser update: %v", err)
	}
	user, err = st.GetUser(ctx)
	if err != nil {
		t.Fatalf("GetUser updated: %v", err)
	}
	if user.PasswordHash != "hash-two" || !user.PasswordChangedAt.Equal(later) {
		t.Fatalf("updated user = %+v, want hash-two at %v", user, later)
	}
	if !user.CreatedAt.Equal(testNow) {
		t.Fatalf("created_at = %v, want preserved %v", user.CreatedAt, testNow)
	}

	if err := st.UpdateUserPassword(ctx, "hash-three", later.Add(time.Hour), testNow); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale UpdateUserPassword error = %v, want ErrConflict", err)
	}
	final := later.Add(time.Hour)
	if err := st.UpdateUserPassword(ctx, "hash-three", final, later); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	user, err = st.GetUser(ctx)
	if err != nil {
		t.Fatalf("GetUser final: %v", err)
	}
	if user.PasswordHash != "hash-three" || !user.PasswordChangedAt.Equal(final) {
		t.Fatalf("final user = %+v, want hash-three at %v", user, final)
	}

	if _, err := sqlDB.Exec(`INSERT INTO users (id, password_hash, password_changed_at, created_at) VALUES (2, 'x', 'x', 'x')`); err == nil {
		t.Fatal("users insert with id=2 succeeded")
	}
}

func TestSessionsCreateLookupTouchAndRevoke(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	session := store.Session{
		ID:         "session-1",
		TokenHash:  hash("raw-token-one"),
		CSRFHash:   hash("raw-csrf-one"),
		CreatedAt:  testNow,
		LastSeenAt: testNow,
		ExpiresAt:  testNow.Add(30 * 24 * time.Hour),
		UserAgent:  "test-agent",
	}
	if err := st.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := st.GetSessionByTokenHash(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	if got.ID != session.ID || got.UserAgent != session.UserAgent || got.RevokedAt != nil {
		t.Fatalf("session = %+v, want id/agent and no revocation", got)
	}
	if !got.CreatedAt.Equal(session.CreatedAt) || !got.LastSeenAt.Equal(session.LastSeenAt) || !got.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatalf("session timestamps = %v/%v/%v, want %v/%v/%v",
			got.CreatedAt, got.LastSeenAt, got.ExpiresAt,
			session.CreatedAt, session.LastSeenAt, session.ExpiresAt)
	}

	if err := st.CreateSession(ctx, store.Session{
		ID:         "session-duplicate",
		TokenHash:  hash("raw-token-one"),
		CSRFHash:   hash("other-csrf"),
		CreatedAt:  testNow,
		LastSeenAt: testNow,
		ExpiresAt:  testNow.Add(time.Hour),
	}); err == nil {
		t.Fatal("CreateSession with duplicate token hash succeeded")
	}

	touched := testNow.Add(5 * time.Minute)
	if err := st.TouchSession(ctx, session.ID, touched); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}
	got, err = st.GetSessionByTokenHash(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash touched: %v", err)
	}
	if !got.LastSeenAt.Equal(touched) {
		t.Fatalf("LastSeenAt = %v, want %v", got.LastSeenAt, touched)
	}

	revokedAt := testNow.Add(time.Hour)
	if err := st.RevokeSession(ctx, session.ID, revokedAt); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	got, err = st.GetSessionByTokenHash(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash revoked: %v", err)
	}
	if got.RevokedAt == nil || !got.RevokedAt.Equal(revokedAt) {
		t.Fatalf("RevokedAt = %v, want %v", got.RevokedAt, revokedAt)
	}
	if err := st.TouchSession(ctx, session.ID, touched); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("TouchSession revoked error = %v, want ErrNotFound", err)
	}
	if err := st.RevokeSession(ctx, session.ID, revokedAt); err != nil {
		t.Fatalf("RevokeSession idempotent: %v", err)
	}
}

func TestSessionsRevokeAllAndOther(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	makeSession := func(id, token string) store.Session {
		return store.Session{
			ID:         id,
			TokenHash:  hash(token),
			CSRFHash:   hash(token + "-csrf"),
			CreatedAt:  testNow,
			LastSeenAt: testNow,
			ExpiresAt:  testNow.Add(time.Hour),
		}
	}
	current := makeSession("current", "current-token")
	other := makeSession("other", "other-token")
	for _, session := range []store.Session{current, other} {
		if err := st.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession(%s): %v", session.ID, err)
		}
	}

	revoked, err := st.RevokeOtherSessions(ctx, current.ID, testNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("RevokeOtherSessions: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("RevokeOtherSessions revoked = %d, want 1", revoked)
	}
	currentRow, err := st.GetSessionByTokenHash(ctx, current.TokenHash)
	if err != nil {
		t.Fatalf("GetSession current: %v", err)
	}
	if currentRow.RevokedAt != nil {
		t.Fatal("current session was revoked")
	}
	otherRow, err := st.GetSessionByTokenHash(ctx, other.TokenHash)
	if err != nil {
		t.Fatalf("GetSession other: %v", err)
	}
	if otherRow.RevokedAt == nil {
		t.Fatal("other session was not revoked")
	}

	revoked, err = st.RevokeAllSessions(ctx, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("RevokeAllSessions: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("RevokeAllSessions revoked = %d, want 1", revoked)
	}
	revoked, err = st.RevokeAllSessions(ctx, testNow.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("RevokeAllSessions second call: %v", err)
	}
	if revoked != 0 {
		t.Fatalf("RevokeAllSessions second call revoked = %d, want 0", revoked)
	}
}

func TestSessionValidation(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	base := store.Session{
		ID:         "session",
		TokenHash:  hash("token"),
		CSRFHash:   hash("csrf"),
		CreatedAt:  testNow,
		LastSeenAt: testNow,
		ExpiresAt:  testNow.Add(time.Hour),
	}
	tests := []struct {
		name    string
		mutate  func(*store.Session)
		wantErr bool
	}{
		{"empty id", func(s *store.Session) { s.ID = "" }, true},
		{"short token hash", func(s *store.Session) { s.TokenHash = []byte("short") }, true},
		{"short csrf hash", func(s *store.Session) { s.CSRFHash = []byte("short") }, true},
		{"zero created", func(s *store.Session) { s.CreatedAt = time.Time{} }, true},
		{"expiry before creation", func(s *store.Session) { s.ExpiresAt = s.CreatedAt }, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := base
			test.mutate(&session)
			err := st.CreateSession(ctx, session)
			if test.wantErr && err == nil {
				t.Fatal("CreateSession succeeded, want error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
		})
	}
}

func TestForeignKeyBehavior(t *testing.T) {
	_, sqlDB := newTestStore(t)
	ctx := context.Background()

	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO monitors
		(id, label, origin_name, destination_name, departure_date, adults, children, infants, enabled, expires_at, created_at, updated_at)
		VALUES ('m1', 'Tehran to Mashhad', 'Tehran', 'Mashhad', '2026-10-30', 1, 0, 0, 1, '2026-10-30T20:30:00Z', '2026-10-24T00:00:00Z', '2026-10-24T00:00:00Z')`); err != nil {
		t.Fatalf("insert monitor: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO monitor_providers
		(monitor_id, provider_id, origin_id, destination_id, created_at, updated_at)
		VALUES ('m1', 'fake', 'origin', 'destination', '2026-10-24T00:00:00Z', '2026-10-24T00:00:00Z')`); err != nil {
		t.Fatalf("insert monitor provider: %v", err)
	}

	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitor_providers WHERE monitor_id = 'm1'`).Scan(&count); err != nil {
		t.Fatalf("count providers: %v", err)
	}
	if count != 1 {
		t.Fatalf("provider count = %d, want 1", count)
	}

	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO monitor_providers
		(monitor_id, provider_id, origin_id, destination_id, created_at, updated_at)
		VALUES ('missing-monitor', 'fake', 'origin', 'destination', '2026-10-24T00:00:00Z', '2026-10-24T00:00:00Z')`); err == nil {
		t.Fatal("insert provider with missing monitor succeeded")
	}

	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM monitors WHERE id = 'm1'`); err != nil {
		t.Fatalf("delete monitor: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM monitor_providers`).Scan(&count); err != nil {
		t.Fatalf("count providers after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("provider count after cascade = %d, want 0", count)
	}

	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&count); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	if count != 0 {
		t.Fatalf("foreign key violations = %d, want 0", count)
	}
}
