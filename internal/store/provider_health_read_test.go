package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func insertProviderHealth(t *testing.T, database *sql.DB, providerID string, fields map[string]any) {
	t.Helper()
	values := map[string]any{
		"provider_id":          providerID,
		"last_attempt_at":      nil,
		"last_success_at":      nil,
		"last_failure_at":      nil,
		"last_error_kind":      nil,
		"last_duration_ms":     nil,
		"consecutive_failures": 0,
		"open_episode_id":      nil,
		"updated_at":           formatTestTime(testNow),
	}
	for key, value := range fields {
		values[key] = value
	}
	if _, err := database.Exec(`INSERT INTO provider_health
		(provider_id, last_attempt_at, last_success_at, last_failure_at,
		 last_error_kind, last_duration_ms, consecutive_failures,
		 open_episode_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		values["provider_id"], values["last_attempt_at"], values["last_success_at"],
		values["last_failure_at"], values["last_error_kind"], values["last_duration_ms"],
		values["consecutive_failures"], values["open_episode_id"], values["updated_at"],
	); err != nil {
		t.Fatalf("insert provider health: %v", err)
	}
}

func TestListProviderHealthReadsDiagnosticsAndOpenEpisode(t *testing.T) {
	st, database := newTestStore(t)
	ctx := context.Background()
	older := testNow.Add(-time.Hour)
	newer := testNow.Add(-time.Minute)
	duration := int64(2500)
	insertProviderHealth(t, database, "a-provider", map[string]any{
		"last_attempt_at":      formatTestTime(newer),
		"last_success_at":      formatTestTime(newer),
		"last_failure_at":      formatTestTime(older),
		"last_error_kind":      "timeout",
		"last_duration_ms":     duration,
		"consecutive_failures": 2,
		"updated_at":           formatTestTime(newer),
	})
	episodeResult, err := database.Exec(`INSERT INTO provider_health_episodes
		(provider_id, opened_at, failure_count_at_open, last_error_kind)
		VALUES ('b-provider', ?, 3, 'network')`, formatTestTime(older))
	if err != nil {
		t.Fatalf("insert health episode: %v", err)
	}
	episodeID, err := episodeResult.LastInsertId()
	if err != nil {
		t.Fatalf("health episode id: %v", err)
	}
	insertProviderHealth(t, database, "b-provider", map[string]any{
		"last_attempt_at":      formatTestTime(newer),
		"last_failure_at":      formatTestTime(newer),
		"last_error_kind":      "network",
		"consecutive_failures": 3,
		"open_episode_id":      episodeID,
		"updated_at":           formatTestTime(newer),
	})

	records, err := st.ListProviderHealth(ctx)
	if err != nil {
		t.Fatalf("ListProviderHealth: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	first := records[0]
	if first.ProviderID != "a-provider" || first.LastSuccessAt == nil ||
		!first.LastSuccessAt.Equal(newer) || first.LastErrorKind != "timeout" ||
		first.LastDurationMS == nil || *first.LastDurationMS != 2500 ||
		first.ConsecutiveFailures != 2 || first.OpenEpisode != nil {
		t.Fatalf("first health record = %+v", first)
	}
	second := records[1]
	if second.ProviderID != "b-provider" || second.LastSuccessAt != nil ||
		second.OpenEpisode == nil || second.OpenEpisode.ID != episodeID ||
		second.OpenEpisode.FailureCountAtOpen != 3 ||
		second.OpenEpisode.LastErrorKind != "network" ||
		!second.OpenEpisode.OpenedAt.Equal(older) {
		t.Fatalf("second health record = %+v", second)
	}

	emptyStore, _ := newTestStore(t)
	empty, err := emptyStore.ListProviderHealth(ctx)
	if err != nil {
		t.Fatalf("ListProviderHealth empty: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty health records = %+v", empty)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := st.ListProviderHealth(canceled); err == nil {
		t.Fatal("ListProviderHealth with canceled context succeeded")
	}
}

func TestListProviderHealthRejectsMissingOpenEpisode(t *testing.T) {
	st, database := newTestStore(t)
	insertProviderHealth(t, database, "broken", map[string]any{
		"consecutive_failures": 3,
		"open_episode_id":      999,
		"updated_at":           formatTestTime(testNow),
	})
	if _, err := st.ListProviderHealth(context.Background()); err == nil {
		t.Fatal("ListProviderHealth accepted a missing open episode")
	}
}
