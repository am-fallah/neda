package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// DefaultTelegramBaseURL is the fixed production Bot API origin.
	DefaultTelegramBaseURL = "https://api.telegram.org"
	// DefaultTelegramTimeout bounds one Telegram API call.
	DefaultTelegramTimeout = 10 * time.Second
	// DefaultTelegramMaxResponseBytes bounds a Bot API response body.
	DefaultTelegramMaxResponseBytes = 64 * 1024
	telegramUserAgent               = "Neda/0.1.0 (self-hosted monitor)"
	telegramMaxMessageRunes         = 4096
)

var errTelegramRedirect = errors.New("notify: telegram redirect rejected")

// TelegramErrorKind classifies Telegram failures for delivery retry policy.
type TelegramErrorKind string

const (
	TelegramErrorPermanent   TelegramErrorKind = "permanent"
	TelegramErrorTransient   TelegramErrorKind = "transient"
	TelegramErrorRateLimited TelegramErrorKind = "rate_limited"
	TelegramErrorCanceled    TelegramErrorKind = "canceled"
)

// TelegramError is a safe Telegram client error. Error() never includes the
// token, request URL, response body, or raw Telegram description.
type TelegramError struct {
	Kind       TelegramErrorKind
	Op         string
	StatusCode int
	RetryAfter time.Duration
	Err        error
}

// Error implements error with safe metadata only.
func (e *TelegramError) Error() string {
	if e == nil {
		return "telegram error"
	}
	parts := []string{"telegram error"}
	if e.Op != "" {
		parts = append(parts, "op="+e.Op)
	}
	kind := e.Kind
	if kind == "" {
		kind = TelegramErrorPermanent
	}
	parts = append(parts, "kind="+string(kind))
	if e.StatusCode != 0 {
		parts = append(parts, "status="+strconv.Itoa(e.StatusCode))
	}
	if e.RetryAfter > 0 {
		parts = append(parts, "retry_after="+e.RetryAfter.String())
	}
	return strings.Join(parts, " ")
}

// Unwrap exposes the underlying cause for errors.Is/As.
func (e *TelegramError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Retryable reports whether the delivery worker may retry this failure.
func (e *TelegramError) Retryable() bool {
	if e == nil {
		return false
	}
	return e.Kind == TelegramErrorTransient || e.Kind == TelegramErrorRateLimited
}

// TelegramClientConfig controls the Bot API client. BaseURL is code-owned and
// normally left zero; tests inject a local server. HTTP clients with redirects
// are copied with redirect following disabled.
type TelegramClientConfig struct {
	BaseURL          *url.URL
	HTTPClient       *http.Client
	Timeout          time.Duration
	MaxResponseBytes int64
}

// TelegramClient is a bounded, redirect-free Telegram Bot API client. It does
// not retry; retry orchestration belongs to the delivery worker.
type TelegramClient struct {
	base        *url.URL
	http        *http.Client
	timeout     time.Duration
	maxResponse int64
}

// TelegramBot is the subset of getMe needed for configuration validation.
type TelegramBot struct {
	ID        int64
	Username  string
	FirstName string
}

// TelegramMessage identifies a sent message without retaining its content.
type TelegramMessage struct {
	MessageID int64
	ChatID    int64
	SentAt    time.Time
}

// NewTelegramClient validates configuration and constructs a client.
func NewTelegramClient(cfg TelegramClientConfig) (*TelegramClient, error) {
	base := cfg.BaseURL
	if base == nil {
		parsed, err := url.Parse(DefaultTelegramBaseURL)
		if err != nil {
			return nil, fmt.Errorf("notify: invalid default telegram base URL: %w", err)
		}
		base = parsed
	}
	if err := validateTelegramBaseURL(base); err != nil {
		return nil, err
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTelegramTimeout
	}
	if cfg.Timeout < 0 || cfg.Timeout > time.Minute {
		return nil, fmt.Errorf("notify: telegram timeout must be between 0 and 60s")
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = DefaultTelegramMaxResponseBytes
	}
	if cfg.MaxResponseBytes < 0 || cfg.MaxResponseBytes > 1024*1024 {
		return nil, fmt.Errorf("notify: telegram response cap must be between 0 and 1 MiB")
	}

	httpClient := http.Client{}
	if cfg.HTTPClient != nil {
		httpClient = *cfg.HTTPClient
	}
	httpClient.Timeout = cfg.Timeout
	httpClient.Jar = nil
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errTelegramRedirect
	}

	cloned := *base
	cloned.Path = strings.TrimRight(cloned.Path, "/")
	return &TelegramClient{
		base:        &cloned,
		http:        &httpClient,
		timeout:     cfg.Timeout,
		maxResponse: cfg.MaxResponseBytes,
	}, nil
}

// GetMe validates the bot token and returns basic bot metadata.
func (c *TelegramClient) GetMe(ctx context.Context, credentials TelegramCredentials) (TelegramBot, error) {
	if c == nil {
		return TelegramBot{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "getMe", Err: errors.New("nil client")}
	}
	if err := credentials.validate(); err != nil {
		return TelegramBot{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "getMe", Err: err}
	}
	body, _, err := c.call(ctx, "getMe", credentials.token, nil)
	if err != nil {
		return TelegramBot{}, err
	}

	var payload struct {
		ID        int64  `json:"id"`
		IsBot     bool   `json:"is_bot"`
		FirstName string `json:"first_name"`
		Username  string `json:"username"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return TelegramBot{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "getMe", Err: fmt.Errorf("invalid result JSON")}
	}
	if payload.ID <= 0 || !payload.IsBot {
		return TelegramBot{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "getMe", Err: errors.New("invalid bot result")}
	}
	return TelegramBot{ID: payload.ID, Username: payload.Username, FirstName: payload.FirstName}, nil
}

// SendMessage sends HTML text to the configured chat. Callers must escape all
// dynamic text with EscapeTelegramHTML before composing the HTML body.
func (c *TelegramClient) SendMessage(ctx context.Context, credentials TelegramCredentials, htmlText string) (TelegramMessage, error) {
	if c == nil {
		return TelegramMessage{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "sendMessage", Err: errors.New("nil client")}
	}
	if err := credentials.validate(); err != nil {
		return TelegramMessage{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "sendMessage", Err: err}
	}
	if strings.TrimSpace(htmlText) == "" {
		return TelegramMessage{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "sendMessage", Err: errors.New("message text is required")}
	}
	if utf8.RuneCountInString(htmlText) > telegramMaxMessageRunes {
		return TelegramMessage{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "sendMessage", Err: errors.New("message text exceeds Telegram's 4096-character limit")}
	}

	form := url.Values{}
	form.Set("chat_id", credentials.chatID)
	form.Set("text", htmlText)
	form.Set("parse_mode", "HTML")
	form.Set("disable_web_page_preview", "true")

	body, _, err := c.call(ctx, "sendMessage", credentials.token, strings.NewReader(form.Encode()))
	if err != nil {
		return TelegramMessage{}, err
	}

	var payload struct {
		MessageID int64 `json:"message_id"`
		Date      int64 `json:"date"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return TelegramMessage{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "sendMessage", Err: fmt.Errorf("invalid result JSON")}
	}
	if payload.MessageID <= 0 || payload.Date <= 0 {
		return TelegramMessage{}, &TelegramError{Kind: TelegramErrorPermanent, Op: "sendMessage", Err: errors.New("invalid message result")}
	}
	return TelegramMessage{
		MessageID: payload.MessageID,
		ChatID:    payload.Chat.ID,
		SentAt:    time.Unix(payload.Date, 0).UTC(),
	}, nil
}

// EscapeTelegramHTML escapes dynamic text for Telegram's HTML parse mode.
func EscapeTelegramHTML(text string) string {
	return html.EscapeString(text)
}

func (c *TelegramClient) call(ctx context.Context, method, token string, body io.Reader) ([]byte, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, telegramContextError(method, err)
	}
	endpoint, err := c.endpoint(method, token)
	if err != nil {
		return nil, 0, &TelegramError{Kind: TelegramErrorPermanent, Op: method, Err: err}
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var request *http.Request
	if body == nil {
		request, err = http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint.String(), nil)
	} else {
		request, err = http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint.String(), body)
		if err == nil {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, 0, &TelegramError{Kind: TelegramErrorPermanent, Op: method, Err: errors.New("invalid request")}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", telegramUserAgent)

	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, errTelegramRedirect) {
			return nil, 0, &TelegramError{Kind: TelegramErrorPermanent, Op: method, StatusCode: http.StatusFound, Err: errors.New("redirect rejected")}
		}
		return nil, 0, telegramTransportError(method, requestCtx, err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests {
		return nil, response.StatusCode, &TelegramError{
			Kind:       TelegramErrorRateLimited,
			Op:         method,
			StatusCode: response.StatusCode,
			RetryAfter: parseTelegramRetryAfter(response.Header.Get("Retry-After")),
			Err:        errors.New("rate limited"),
		}
	}
	if response.StatusCode >= 500 {
		return nil, response.StatusCode, &TelegramError{
			Kind:       TelegramErrorTransient,
			Op:         method,
			StatusCode: response.StatusCode,
			Err:        errors.New("server error"),
		}
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, response.StatusCode, &TelegramError{
			Kind:       TelegramErrorPermanent,
			Op:         method,
			StatusCode: response.StatusCode,
			Err:        errors.New("request rejected"),
		}
	}

	payload, err := readTelegramBody(response.Body, c.maxResponse)
	if err != nil {
		return nil, response.StatusCode, &TelegramError{Kind: TelegramErrorPermanent, Op: method, StatusCode: response.StatusCode, Err: err}
	}
	var envelope struct {
		OK          *bool           `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, response.StatusCode, &TelegramError{Kind: TelegramErrorPermanent, Op: method, StatusCode: response.StatusCode, Err: errors.New("invalid response JSON")}
	}
	if envelope.OK == nil {
		return nil, response.StatusCode, &TelegramError{Kind: TelegramErrorPermanent, Op: method, StatusCode: response.StatusCode, Err: errors.New("response is missing ok")}
	}
	if !*envelope.OK {
		kind, retryAfter := classifyTelegramAPIError(envelope.ErrorCode)
		if kind == TelegramErrorRateLimited {
			retryAfter = parseTelegramRetryAfter(response.Header.Get("Retry-After"))
		}
		return nil, response.StatusCode, &TelegramError{
			Kind:       kind,
			Op:         method,
			StatusCode: envelope.ErrorCode,
			RetryAfter: retryAfter,
			Err:        errors.New("telegram rejected the request"),
		}
	}
	return envelope.Result, response.StatusCode, nil
}

func (c *TelegramClient) endpoint(method, token string) (*url.URL, error) {
	if err := validateTelegramToken(token); err != nil {
		return nil, err
	}
	if strings.TrimSpace(method) == "" || strings.ContainsAny(method, "/?#") {
		return nil, errors.New("invalid method")
	}
	endpoint := *c.base
	endpoint.Path = c.base.Path + "/bot" + token + "/" + method
	return &endpoint, nil
}

func validateTelegramBaseURL(base *url.URL) error {
	if base == nil {
		return errors.New("notify: telegram base URL is required")
	}
	if base.Scheme != "https" && !(base.Scheme == "http" && isLoopbackHost(base.Hostname())) {
		return errors.New("notify: telegram base URL must use https or a loopback test server")
	}
	if base.Host == "" {
		return errors.New("notify: telegram base URL host is required")
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("notify: telegram base URL must not contain userinfo, query, or fragment")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func readTelegramBody(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, errors.New("invalid response limit")
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, errors.New("response body could not be read")
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return body, nil
}

func telegramTransportError(method string, ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return telegramContextError(method, ctxErr)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &TelegramError{Kind: TelegramErrorTransient, Op: method, Err: errors.New("timeout")}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &TelegramError{Kind: TelegramErrorTransient, Op: method, Err: errors.New("timeout")}
	}
	return &TelegramError{Kind: TelegramErrorTransient, Op: method, Err: errors.New("network error")}
}

func telegramContextError(method string, err error) error {
	if errors.Is(err, context.Canceled) {
		return &TelegramError{Kind: TelegramErrorCanceled, Op: method, Err: context.Canceled}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &TelegramError{Kind: TelegramErrorTransient, Op: method, Err: context.DeadlineExceeded}
	}
	return &TelegramError{Kind: TelegramErrorTransient, Op: method, Err: errors.New("request canceled")}
}

func classifyTelegramAPIError(code int) (TelegramErrorKind, time.Duration) {
	switch {
	case code == http.StatusTooManyRequests:
		return TelegramErrorRateLimited, 0
	case code >= 500:
		return TelegramErrorTransient, 0
	default:
		return TelegramErrorPermanent, 0
	}
}

func parseTelegramRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds > 24*60*60 {
			seconds = 24 * 60 * 60
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		delay := time.Until(when)
		if delay > 0 {
			return delay
		}
	}
	return 0
}
