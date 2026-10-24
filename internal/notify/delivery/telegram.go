package delivery

import (
	"context"
	"errors"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
)

// TelegramSender adapts encrypted Telegram settings and the bounded Bot API
// client to the delivery worker. It never logs or returns credentials.
type TelegramSender struct {
	settings *telegramsettings.Settings
	client   *notify.TelegramClient
}

// NewTelegramSender validates dependencies.
func NewTelegramSender(settings *telegramsettings.Settings, client *notify.TelegramClient) (*TelegramSender, error) {
	if settings == nil {
		return nil, errors.New("delivery: nil telegram settings")
	}
	if client == nil {
		return nil, errors.New("delivery: nil telegram client")
	}
	return &TelegramSender{settings: settings, client: client}, nil
}

// Send implements Sender.
func (s *TelegramSender) Send(ctx context.Context, record Record) Result {
	if s == nil || s.settings == nil || s.client == nil {
		return Failed("telegram_not_initialized")
	}
	if record.Channel != notify.ChannelTelegram {
		return Failed("telegram_wrong_channel")
	}
	credentials, err := s.settings.Load(ctx)
	if err != nil {
		switch {
		case errors.Is(err, telegramsettings.ErrTelegramNotConfigured):
			return Skipped("telegram_not_configured")
		case errors.Is(err, telegramsettings.ErrTelegramSettingsUnreadable):
			return Skipped("telegram_unreadable")
		default:
			return Retry("telegram_settings_read_failed", 0)
		}
	}
	text, err := notify.FormatTelegram(record.Event)
	if err != nil {
		return Failed("telegram_format_failed")
	}
	if _, err := s.client.SendMessage(ctx, credentials, text); err != nil {
		var telegramErr *notify.TelegramError
		if !errors.As(err, &telegramErr) || telegramErr == nil {
			return Failed("telegram_unknown")
		}
		switch telegramErr.Kind {
		case notify.TelegramErrorRateLimited:
			return Retry("telegram_rate_limited", telegramErr.RetryAfter)
		case notify.TelegramErrorTransient:
			return Retry("telegram_transient", telegramErr.RetryAfter)
		case notify.TelegramErrorCanceled:
			return Retry("telegram_canceled", 0)
		default:
			return Failed("telegram_permanent")
		}
	}
	return Sent()
}
