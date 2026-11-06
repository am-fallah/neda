package store_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/store"
)

func TestProviderRateLimitRoundTripAndDefaultScope(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	providerID := provider.ID("provider-rate")

	missing, found, err := st.GetProviderRateLimit(ctx, providerID, "")
	if err != nil {
		t.Fatalf("GetProviderRateLimit(missing): %v", err)
	}
	if found {
		t.Fatal("missing provider rate limit reported found")
	}
	if missing.ProviderID != providerID || missing.Scope != domain.ProviderRateLimitScopeProvider {
		t.Fatalf("missing state = %+v", missing)
	}

	cooldown := testNow.Add(20 * time.Minute)
	last429 := testNow.Add(-time.Minute)
	lastSuccess := testNow.Add(-2 * time.Minute)
	state := domain.ProviderRateLimit{
		ProviderID:           providerID,
		Consecutive429s:      2,
		AdaptiveInterval:     20 * time.Minute,
		CooldownUntil:        &cooldown,
		ConsecutiveSuccesses: 1,
		Last429At:            &last429,
		LastSuccessAt:        &lastSuccess,
		LastRetryAfter:       90 * time.Second,
	}
	if err := st.UpsertProviderRateLimit(ctx, state, testNow); err != nil {
		t.Fatalf("UpsertProviderRateLimit: %v", err)
	}

	loaded, found, err := st.GetProviderRateLimit(ctx, providerID, domain.ProviderRateLimitScopeProvider)
	if err != nil {
		t.Fatalf("GetProviderRateLimit: %v", err)
	}
	if !found {
		t.Fatal("provider rate limit not found after upsert")
	}
	if loaded.ProviderID != providerID || loaded.Scope != domain.ProviderRateLimitScopeProvider ||
		loaded.Consecutive429s != 2 || loaded.AdaptiveInterval != 20*time.Minute ||
		loaded.ConsecutiveSuccesses != 1 || loaded.LastRetryAfter != 90*time.Second ||
		loaded.CooldownUntil == nil || !loaded.CooldownUntil.Equal(cooldown) ||
		loaded.Last429At == nil || !loaded.Last429At.Equal(last429) ||
		loaded.LastSuccessAt == nil || !loaded.LastSuccessAt.Equal(lastSuccess) {
		t.Fatalf("loaded state = %+v", loaded)
	}
	if !loaded.UpdatedAt.Equal(testNow) {
		t.Fatalf("updated_at = %v, want %v", loaded.UpdatedAt, testNow)
	}

	state.AdaptiveInterval = 10 * time.Minute
	state.Consecutive429s = 1
	if err := st.UpsertProviderRateLimit(ctx, state, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("UpsertProviderRateLimit(update): %v", err)
	}
	loaded, found, err = st.GetProviderRateLimit(ctx, providerID, "")
	if err != nil || !found {
		t.Fatalf("GetProviderRateLimit(update) = %+v, %v, %v", loaded, found, err)
	}
	if loaded.AdaptiveInterval != 10*time.Minute || loaded.Consecutive429s != 1 {
		t.Fatalf("updated state = %+v", loaded)
	}

	other := domain.ProviderRateLimit{
		ProviderID:       "another-provider",
		AdaptiveInterval: time.Minute,
	}
	if err := st.UpsertProviderRateLimit(ctx, other, testNow); err != nil {
		t.Fatalf("UpsertProviderRateLimit(other): %v", err)
	}
	all, err := st.ListProviderRateLimits(ctx)
	if err != nil {
		t.Fatalf("ListProviderRateLimits: %v", err)
	}
	if len(all) != 2 || all[0].ProviderID != "another-provider" || all[1].ProviderID != providerID {
		t.Fatalf("rate limits = %+v", all)
	}
}

func TestProviderRateLimitTransactionRollbackAndValidation(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	state := domain.ProviderRateLimit{
		ProviderID:       "rollback",
		AdaptiveInterval: time.Minute,
	}
	err := st.WithSQLTx(ctx, func(_ *sql.Tx, tx *store.Store) error {
		if err := tx.UpsertProviderRateLimit(ctx, state, testNow); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("WithSQLTx succeeded")
	}
	if _, found, err := st.GetProviderRateLimit(ctx, "rollback", ""); err != nil || found {
		t.Fatalf("rolled-back state found=%v err=%v", found, err)
	}

	valid := domain.ProviderRateLimit{ProviderID: "valid"}
	cases := []struct {
		name  string
		state domain.ProviderRateLimit
		now   time.Time
	}{
		{name: "empty provider", state: domain.ProviderRateLimit{}, now: testNow},
		{name: "long scope", state: domain.ProviderRateLimit{ProviderID: "valid", Scope: strings.Repeat("x", 65)}, now: testNow},
		{name: "negative 429 count", state: domain.ProviderRateLimit{ProviderID: "valid", Consecutive429s: -1}, now: testNow},
		{name: "negative adaptive interval", state: domain.ProviderRateLimit{ProviderID: "valid", AdaptiveInterval: -time.Second}, now: testNow},
		{name: "negative successes", state: domain.ProviderRateLimit{ProviderID: "valid", ConsecutiveSuccesses: -1}, now: testNow},
		{name: "negative retry after", state: domain.ProviderRateLimit{ProviderID: "valid", LastRetryAfter: -time.Second}, now: testNow},
		{name: "zero now", state: valid, now: time.Time{}},
	}
	for _, test := range cases {
		if err := st.UpsertProviderRateLimit(ctx, test.state, test.now); err == nil {
			t.Fatalf("%s accepted", test.name)
		}
	}
	if _, _, err := st.GetProviderRateLimit(ctx, "", ""); err == nil {
		t.Fatal("GetProviderRateLimit accepted an empty provider")
	}
}
