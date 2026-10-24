package push

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/delivery"
)

var (
	// ErrPushDisabled reports that the global Push channel is switched off.
	ErrPushDisabled = errors.New("push: channel is disabled")
	// ErrNoSubscriptions reports that a test had no active device to target.
	ErrNoSubscriptions = errors.New("push: no active subscriptions")
)

const (
	defaultPushTTL       = 3600
	maxTestSubscriptions = 20
	testTitle            = "Neda test notification"
	testBody             = "Web Push is configured. Availability and provider-health events will arrive here."
)

// SenderConfig controls the bounded Push sender. Resolver and HTTPClient are
// injectable for deterministic tests; production leaves both zero.
type SenderConfig struct {
	Resolver   Resolver
	HTTPClient *http.Client
	Now        func() time.Time
	PublicURL  string
	TTL        int
	Timeout    time.Duration
}

// Sender performs server-side Web Push delivery. It validates the endpoint and
// resolved/connected IPs for every send, disables gone subscriptions, and does
// not retry internally.
type Sender struct {
	service   *Service
	client    *http.Client
	now       func() time.Time
	publicURL string
	ttl       int
	timeout   time.Duration
	validate  func(string) error
}

// NewSender validates dependencies and constructs a sender.
func NewSender(service *Service, cfg SenderConfig) (*Sender, error) {
	if service == nil {
		return nil, errors.New("push: nil service")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultSendTimeout
	}
	if cfg.Timeout < time.Second || cfg.Timeout > time.Minute {
		return nil, errors.New("push: send timeout must be between 1s and 1m")
	}
	if cfg.TTL == 0 {
		cfg.TTL = defaultPushTTL
	}
	if cfg.TTL < 0 || cfg.TTL > 24*3600 {
		return nil, errors.New("push: TTL must be between 0 and 24h")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = newHTTPClient(cfg.Resolver, cfg.Timeout)
	}
	return &Sender{
		service:   service,
		client:    client,
		now:       cfg.Now,
		publicURL: strings.TrimSpace(cfg.PublicURL),
		ttl:       cfg.TTL,
		timeout:   cfg.Timeout,
		validate:  ValidateEndpoint,
	}, nil
}

// Send implements delivery.Sender. It never logs or returns endpoint/key
// material; result categories are stable and safe.
func (s *Sender) Send(ctx context.Context, record delivery.Record) delivery.Result {
	if s == nil || s.service == nil {
		return delivery.Failed("push_not_initialized")
	}
	if record.Channel != notify.ChannelPush {
		return delivery.Failed("push_wrong_channel")
	}
	if err := ctx.Err(); err != nil {
		return delivery.Retry("push_canceled", 0)
	}
	enabled, err := s.service.store.PushEnabled(ctx)
	if err != nil {
		return delivery.Retry("push_setting_read_failed", 0)
	}
	if !enabled {
		return delivery.Skipped("push_disabled")
	}
	id := strings.TrimPrefix(record.TargetKey, "push:")
	if id == record.TargetKey || strings.TrimSpace(id) == "" {
		return delivery.Failed("push_invalid_target")
	}
	subscription, err := s.service.Load(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, ErrDisabled):
			return delivery.Skipped("push_subscription_disabled")
		case errors.Is(err, ErrNotFound):
			return delivery.Failed("push_subscription_missing")
		default:
			return delivery.Failed("push_subscription_unreadable")
		}
	}
	payload, err := notify.FormatPush(record.Event, s.publicURL)
	if err != nil {
		return delivery.Failed("push_format_failed")
	}
	result, disable := s.sendTo(ctx, subscription, payload)
	s.recordResult(ctx, subscription.ID, result, disable)
	return result
}

// TestOutcome reports a sanitized test result for one or more devices.
type TestOutcome struct {
	Attempted  int
	Sent       int
	Failed     int
	ErrorKinds []string
}

// Test sends one static notification to a device, or to all active devices
// when subscriptionID is empty. It never creates an event or outbox row.
func (s *Sender) Test(ctx context.Context, subscriptionID string) (TestOutcome, error) {
	if s == nil || s.service == nil {
		return TestOutcome{}, ErrUnavailable
	}
	enabled, err := s.service.store.PushEnabled(ctx)
	if err != nil {
		return TestOutcome{}, err
	}
	if !enabled {
		return TestOutcome{}, ErrPushDisabled
	}
	ids := make([]string, 0, 1)
	if id := strings.TrimSpace(subscriptionID); id != "" {
		ids = append(ids, id)
	} else {
		subscriptions, err := s.service.List(ctx)
		if err != nil {
			return TestOutcome{}, err
		}
		for _, subscription := range subscriptions {
			if len(ids) >= maxTestSubscriptions {
				break
			}
			ids = append(ids, subscription.ID)
		}
	}
	if len(ids) == 0 {
		return TestOutcome{}, ErrNoSubscriptions
	}
	payload, err := json.Marshal(notify.PushPayload{
		Title: testTitle,
		Body:  testBody,
		URL:   testURL(s.publicURL),
		Tag:   "neda-test",
		Kind:  "test",
	})
	if err != nil {
		return TestOutcome{}, err
	}
	outcome := TestOutcome{Attempted: len(ids)}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			outcome.Failed++
			outcome.ErrorKinds = append(outcome.ErrorKinds, "canceled")
			continue
		}
		subscription, err := s.service.Load(ctx, id)
		if err != nil {
			outcome.Failed++
			switch {
			case errors.Is(err, ErrDisabled), errors.Is(err, ErrNotFound):
				outcome.ErrorKinds = append(outcome.ErrorKinds, "subscription_unavailable")
			default:
				outcome.ErrorKinds = append(outcome.ErrorKinds, "subscription_unreadable")
			}
			continue
		}
		result, disable := s.sendTo(ctx, subscription, payload)
		s.recordResult(ctx, subscription.ID, result, disable)
		if result.Outcome == delivery.OutcomeSent {
			outcome.Sent++
			continue
		}
		outcome.Failed++
		kind := result.ErrorKind
		if kind == "" {
			kind = "send_failed"
		}
		outcome.ErrorKinds = append(outcome.ErrorKinds, kind)
	}
	return outcome, nil
}

func (s *Sender) sendTo(ctx context.Context, subscription LoadedSubscription, payload []byte) (delivery.Result, bool) {
	if s.validate != nil {
		if err := s.validate(subscription.Endpoint); err != nil {
			return delivery.Failed("push_endpoint_rejected"), false
		}
	}
	privateKey, publicKey, err := s.service.vapid.privateKeyForSend(ctx)
	if err != nil {
		return delivery.Failed("push_vapid_unavailable"), false
	}
	sendCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	response, err := webpush.SendNotificationWithContext(sendCtx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys: webpush.Keys{
			P256dh: subscription.P256DH,
			Auth:   subscription.Auth,
		},
	}, &webpush.Options{
		HTTPClient:      s.client,
		Subscriber:      s.service.vapid.subjectValue(),
		TTL:             s.ttl,
		Urgency:         webpush.UrgencyHigh,
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
	})
	if err != nil {
		return classifyPushError(err), false
	}
	if response == nil {
		return delivery.Retry("push_empty_response", 0), false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxPushResponseBody))
	status := response.StatusCode
	switch {
	case status >= 200 && status <= 204:
		return delivery.Sent(), false
	case status == http.StatusNotFound || status == http.StatusGone:
		return delivery.Failed("push_gone"), true
	case status == http.StatusTooManyRequests:
		return delivery.Retry("push_rate_limited", retryAfter(response.Header.Get("Retry-After"))), false
	case status >= 500:
		return delivery.Retry("push_transient", retryAfter(response.Header.Get("Retry-After"))), false
	case status == http.StatusBadRequest || status == http.StatusUnauthorized ||
		status == http.StatusForbidden || status == http.StatusRequestEntityTooLarge:
		return delivery.Failed("push_permanent"), false
	default:
		return delivery.Failed("push_rejected"), false
	}
}

func (s *Sender) recordResult(ctx context.Context, id string, result delivery.Result, disable bool) {
	now := s.now().UTC()
	switch result.Outcome {
	case delivery.OutcomeSent:
		_ = s.service.MarkSuccess(ctx, id, now)
	default:
		if result.Outcome == delivery.OutcomeRetry || result.Outcome == delivery.OutcomeFailed {
			_ = s.service.MarkFailure(ctx, id, disable, now)
		}
	}
}

func classifyPushError(err error) delivery.Result {
	switch {
	case errors.Is(err, errPushRedirect):
		return delivery.Failed("push_redirect_rejected")
	case errors.Is(err, context.Canceled):
		return delivery.Retry("push_canceled", 0)
	case errors.Is(err, context.DeadlineExceeded):
		return delivery.Retry("push_timeout", 0)
	case errors.Is(err, errBlockedAddress):
		return delivery.Failed("push_blocked_address")
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return delivery.Retry("push_timeout", 0)
	}
	if errors.As(err, &netErr) {
		return delivery.Retry("push_network", 0)
	}
	return delivery.Failed("push_send_failed")
}

func retryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		delay := time.Duration(seconds) * time.Second
		if delay > 2*time.Hour {
			return 2 * time.Hour
		}
		return delay
	}
	if when, err := http.ParseTime(raw); err == nil {
		delay := time.Until(when)
		if delay <= 0 {
			return 0
		}
		if delay > 2*time.Hour {
			return 2 * time.Hour
		}
		return delay
	}
	return 0
}

func testURL(publicURL string) string {
	base := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if base == "" {
		return ""
	}
	return base + "/settings"
}
