// Package mrbilit implements the deterministic MrBilit adapter foundation.
//
// The request endpoint and parameter names are the documented/provisional
// contract from docs/BRIEF.md and docs/ARCHITECTURE.md. The response envelope,
// offering container key, time formats, and capacity semantics are NOT
// verified. The parser therefore accepts only an explicit synthetic contract
// and fails closed on every other shape. No production registration is done
// here; bounded live validation remains PRV-006/GATE-003.
package mrbilit

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

// Config controls the adapter. BaseURL is code-owned and normally left zero;
// tests inject a loopback httptest server. The transport never creates an
// overall check deadline; SCH-004 owns the caller context.
type Config struct {
	BaseURL          *url.URL
	HTTPClient       *http.Client
	AttemptTimeout   time.Duration
	MaxResponseBytes int64
	MaxRetries       int
	RetryBackoff     []time.Duration
	Jitter           func(time.Duration) time.Duration
	Sleep            func(context.Context, time.Duration) error
	Logger           *slog.Logger
	Now              func() time.Time
}

// Provider implements provider.Provider for MrBilit.
type Provider struct {
	client *httpClient
	now    func() time.Time
}

// New validates configuration and constructs an unregistered adapter.
func New(cfg Config) (*Provider, error) {
	client, err := newHTTPClient(clientConfig{
		BaseURL:          cfg.BaseURL,
		HTTPClient:       cfg.HTTPClient,
		AttemptTimeout:   cfg.AttemptTimeout,
		MaxResponseBytes: cfg.MaxResponseBytes,
		MaxRetries:       cfg.MaxRetries,
		RetryBackoff:     cfg.RetryBackoff,
		Jitter:           cfg.Jitter,
		Sleep:            cfg.Sleep,
		Logger:           cfg.Logger,
	})
	if err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Provider{client: client, now: now}, nil
}

// Metadata returns code-owned provider metadata.
func (p *Provider) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          ID,
		DisplayName: DisplayName,
		TestOnly:    false,
		Capabilities: provider.Capabilities{
			RequiresOriginID:      true,
			RequiresDestinationID: true,
			SupportsBookingURL:    false,
			SupportsGender:        false,
			MaxPassengers:         maxPassengers,
		},
	}
}

// ValidateConfig implements the provider interface.
func (p *Provider) ValidateConfig(cfg provider.MonitorProviderConfig) error {
	return ValidateConfig(cfg)
}

// Check builds one availability request and parses the synthetic response
// contract. It honors the caller's context deadline and never creates a
// competing overall deadline.
func (p *Provider) Check(ctx context.Context, req provider.CheckRequest) (provider.Result, error) {
	if p == nil || p.client == nil {
		return provider.Result{}, errors.New("mrbilit: provider is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return provider.Result{}, contextError(err)
	}
	if err := validateCheckRequest(req); err != nil {
		return provider.Result{}, err
	}
	endpoint, err := buildURL(p.client.base, req)
	if err != nil {
		return provider.Result{}, err
	}
	body, _, err := p.client.get(ctx, endpoint)
	if err != nil {
		return provider.Result{}, err
	}
	result, err := parseSyntheticResponse(body, req, p.now().UTC())
	if err != nil {
		return provider.Result{}, err
	}
	if err := provider.ValidateResult(result); err != nil {
		return provider.Result{}, parseError("invalid_normalized_result")
	}
	return result, nil
}
