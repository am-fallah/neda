package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/provider/fake"
)

type stubProvider struct {
	metadata provider.Metadata
	err      error
}

func (s stubProvider) Metadata() provider.Metadata { return s.metadata }
func (s stubProvider) ValidateConfig(provider.MonitorProviderConfig) error {
	return s.err
}
func (s stubProvider) Check(context.Context, provider.CheckRequest) (provider.Result, error) {
	return provider.Result{}, s.err
}

func TestStateAndErrorKindValidation(t *testing.T) {
	for _, state := range []provider.State{provider.StateReservable, provider.StateNotReservable, provider.StateUnknown} {
		if !state.Valid() {
			t.Fatalf("State(%q).Valid() = false", state)
		}
	}
	if provider.State("bad").Valid() {
		t.Fatal("invalid State.Valid() = true")
	}

	for _, kind := range []provider.ErrorKind{
		provider.ErrTimeout,
		provider.ErrNetwork,
		provider.ErrHTTPStatus,
		provider.ErrRateLimited,
		provider.ErrInvalidResponse,
		provider.ErrRouteNotFound,
		provider.ErrConfiguration,
		provider.ErrCanceled,
	} {
		if !kind.Valid() {
			t.Fatalf("ErrorKind(%q).Valid() = false", kind)
		}
	}
	if provider.ErrorKind("bad").Valid() {
		t.Fatal("invalid ErrorKind.Valid() = true")
	}
}

func TestProviderErrorIsSafeAndUnwraps(t *testing.T) {
	underlying := errors.New("https://secret.example/path?token=abc")
	err := &provider.Error{
		Kind:       provider.ErrNetwork,
		Op:         "Check",
		StatusCode: 503,
		RetryAfter: 2 * time.Second,
		Err:        underlying,
	}
	message := err.Error()
	for _, secret := range []string{"secret.example", "token", "abc", "https://"} {
		if strings.Contains(message, secret) {
			t.Fatalf("Error() leaked %q: %q", secret, message)
		}
	}
	for _, expected := range []string{"kind=network", "status=503", "retry_after=2s"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("Error() = %q, want %q", message, expected)
		}
	}
	if !errors.Is(err, underlying) {
		t.Fatal("errors.Is did not find underlying error")
	}
	if err.Unwrap() != underlying {
		t.Fatal("Unwrap did not return underlying error")
	}

	var nilError *provider.Error
	if got := nilError.Error(); got != "provider error" {
		t.Fatalf("nil Error() = %q", got)
	}
	if nilError.Unwrap() != nil {
		t.Fatal("nil Unwrap did not return nil")
	}
}

func TestRegistry(t *testing.T) {
	fakeProvider := fake.New()
	other := stubProvider{metadata: provider.Metadata{ID: "other", DisplayName: "Other provider"}}
	registry, err := provider.NewRegistry(fakeProvider, other)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if got, ok := registry.Lookup(fake.ID); !ok || got != fakeProvider {
		t.Fatalf("Lookup(%q) = %v, %v", fake.ID, got, ok)
	}
	if _, ok := registry.Lookup("missing"); ok {
		t.Fatal("Lookup(missing) succeeded")
	}
	list := registry.List()
	if len(list) != 2 || list[0].ID != fake.ID || list[1].ID != "other" {
		t.Fatalf("List() = %+v, want registration order", list)
	}
	list[0].DisplayName = "mutated"
	if registry.List()[0].DisplayName == "mutated" {
		t.Fatal("List() exposed mutable registry metadata")
	}
	if list[0].TestOnly != true {
		t.Fatal("fake provider metadata is not marked TestOnly")
	}
}

func TestRegistryRejectsInvalidProviders(t *testing.T) {
	tests := []struct {
		name      string
		providers []provider.Provider
	}{
		{"nil", []provider.Provider{nil}},
		{"empty id", []provider.Provider{stubProvider{metadata: provider.Metadata{ID: "", DisplayName: "x"}}}},
		{"whitespace id", []provider.Provider{stubProvider{metadata: provider.Metadata{ID: " x ", DisplayName: "x"}}}},
		{"empty display name", []provider.Provider{stubProvider{metadata: provider.Metadata{ID: "x", DisplayName: " "}}}},
		{"duplicate", []provider.Provider{
			stubProvider{metadata: provider.Metadata{ID: "dup", DisplayName: "One"}},
			stubProvider{metadata: provider.Metadata{ID: "dup", DisplayName: "Two"}},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := provider.NewRegistry(test.providers...); err == nil {
				t.Fatal("NewRegistry succeeded, want error")
			}
		})
	}
}

func TestRegistryAllowsEmptyAndNilReceiver(t *testing.T) {
	registry, err := provider.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry empty: %v", err)
	}
	if len(registry.List()) != 0 {
		t.Fatal("empty registry List() is not empty")
	}
	if _, ok := registry.Lookup("missing"); ok {
		t.Fatal("empty registry Lookup succeeded")
	}
	var nilRegistry *provider.Registry
	if _, ok := nilRegistry.Lookup("missing"); ok {
		t.Fatal("nil registry Lookup succeeded")
	}
	if nilRegistry.List() != nil {
		t.Fatal("nil registry List() != nil")
	}
}

func TestClassify(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()

	tests := []struct {
		name         string
		ctx          context.Context
		err          error
		wantCategory provider.Category
		wantKind     provider.ErrorKind
		wantStatus   int
		wantRetry    time.Duration
		wantOutage   bool
		wantBackoff  bool
	}{
		{"success", context.Background(), nil, provider.CategorySuccess, "", 0, 0, false, false},
		{"caller canceled", canceledCtx, nil, provider.CategoryAbandoned, provider.ErrCanceled, 0, 0, false, false},
		{"caller deadline", deadlineCtx, nil, provider.CategoryInfrastructure, provider.ErrTimeout, 0, 0, true, true},
		{"canceled wins over config", canceledCtx, &provider.Error{Kind: provider.ErrConfiguration}, provider.CategoryAbandoned, provider.ErrCanceled, 0, 0, false, false},
		{"deadline wins over config", deadlineCtx, &provider.Error{Kind: provider.ErrConfiguration}, provider.CategoryInfrastructure, provider.ErrTimeout, 0, 0, true, true},
		{"context canceled error without caller cancellation", context.Background(), context.Canceled, provider.CategoryInfrastructure, provider.ErrInvalidResponse, 0, 0, true, true},
		{"context deadline error", context.Background(), context.DeadlineExceeded, provider.CategoryInfrastructure, provider.ErrTimeout, 0, 0, true, true},
		{"timeout", context.Background(), &provider.Error{Kind: provider.ErrTimeout}, provider.CategoryInfrastructure, provider.ErrTimeout, 0, 0, true, true},
		{"network", context.Background(), &provider.Error{Kind: provider.ErrNetwork}, provider.CategoryInfrastructure, provider.ErrNetwork, 0, 0, true, true},
		{"rate limited", context.Background(), &provider.Error{Kind: provider.ErrRateLimited, RetryAfter: 2 * time.Second}, provider.CategoryInfrastructure, provider.ErrRateLimited, 0, 2 * time.Second, true, true},
		{"http 429", context.Background(), &provider.Error{Kind: provider.ErrHTTPStatus, StatusCode: 429, RetryAfter: 3 * time.Second}, provider.CategoryInfrastructure, provider.ErrRateLimited, 429, 3 * time.Second, true, true},
		{"http 500", context.Background(), &provider.Error{Kind: provider.ErrHTTPStatus, StatusCode: 500}, provider.CategoryInfrastructure, provider.ErrHTTPStatus, 500, 0, true, true},
		{"http 404", context.Background(), &provider.Error{Kind: provider.ErrHTTPStatus, StatusCode: 404}, provider.CategoryRequest, provider.ErrHTTPStatus, 404, 0, false, false},
		{"http 302", context.Background(), &provider.Error{Kind: provider.ErrHTTPStatus, StatusCode: 302}, provider.CategoryInfrastructure, provider.ErrInvalidResponse, 302, 0, true, true},
		{"route not found", context.Background(), &provider.Error{Kind: provider.ErrRouteNotFound}, provider.CategoryRoute, provider.ErrRouteNotFound, 0, 0, false, false},
		{"configuration", context.Background(), &provider.Error{Kind: provider.ErrConfiguration}, provider.CategoryConfiguration, provider.ErrConfiguration, 0, 0, false, false},
		{"invalid response", context.Background(), &provider.Error{Kind: provider.ErrInvalidResponse}, provider.CategoryInfrastructure, provider.ErrInvalidResponse, 0, 0, true, true},
		{"adapter canceled without caller cancellation", context.Background(), &provider.Error{Kind: provider.ErrCanceled}, provider.CategoryInfrastructure, provider.ErrInvalidResponse, 0, 0, true, true},
		{"unknown error", context.Background(), errors.New("boom"), provider.CategoryInfrastructure, provider.ErrInvalidResponse, 0, 0, true, true},
		{"wrapped network", context.Background(), errors.Join(errors.New("outer"), &provider.Error{Kind: provider.ErrNetwork}), provider.CategoryInfrastructure, provider.ErrNetwork, 0, 0, true, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := provider.Classify(test.ctx, test.err)
			if got.Category != test.wantCategory {
				t.Fatalf("Category = %q, want %q", got.Category, test.wantCategory)
			}
			if got.Kind != test.wantKind {
				t.Fatalf("Kind = %q, want %q", got.Kind, test.wantKind)
			}
			if got.StatusCode != test.wantStatus {
				t.Fatalf("StatusCode = %d, want %d", got.StatusCode, test.wantStatus)
			}
			if got.RetryAfter != test.wantRetry {
				t.Fatalf("RetryAfter = %v, want %v", got.RetryAfter, test.wantRetry)
			}
			if got.CountsTowardOutage != test.wantOutage {
				t.Fatalf("CountsTowardOutage = %v, want %v", got.CountsTowardOutage, test.wantOutage)
			}
			if got.TargetBackoff != test.wantBackoff {
				t.Fatalf("TargetBackoff = %v, want %v", got.TargetBackoff, test.wantBackoff)
			}
		})
	}
}

func TestClassificationHelpers(t *testing.T) {
	if !(provider.Classification{Category: provider.CategorySuccess}).IsSuccess() {
		t.Fatal("success classification helper failed")
	}
	if !(provider.Classification{Category: provider.CategoryInfrastructure}).IsInfrastructure() {
		t.Fatal("infrastructure classification helper failed")
	}
	if !(provider.Classification{Category: provider.CategoryAbandoned}).IsAbandoned() {
		t.Fatal("abandoned classification helper failed")
	}
}
