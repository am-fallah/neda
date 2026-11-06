package report

import (
	"context"
	"errors"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
)

// ErrTelegramUnavailable reports that the encrypted Telegram subsystem is not
// configured in this process. The report row is marked skipped, not retried.
var ErrTelegramUnavailable = errors.New("report: telegram settings unavailable")

// TelegramSender sends the daily report silently through the existing encrypted
// Telegram configuration. It never returns credentials or message text.
type TelegramSender struct {
	settings *telegramsettings.Settings
	client   *notify.TelegramClient
}

// NewTelegramSender validates dependencies.
func NewTelegramSender(settings *telegramsettings.Settings, client *notify.TelegramClient) (*TelegramSender, error) {
	if settings == nil {
		return nil, ErrTelegramUnavailable
	}
	if client == nil {
		return nil, errors.New("report: nil telegram client")
	}
	return &TelegramSender{settings: settings, client: client}, nil
}

// Send implements Sender.
func (s *TelegramSender) Send(ctx context.Context, text string) (int64, error) {
	if s == nil || s.settings == nil || s.client == nil {
		return 0, ErrTelegramUnavailable
	}
	credentials, err := s.settings.Load(ctx)
	if err != nil {
		return 0, err
	}
	message, err := s.client.SendMessageWithOptions(ctx, credentials, text, notify.SendMessageOptions{
		DisableNotification: true,
	})
	if err != nil {
		return 0, err
	}
	return message.MessageID, nil
}
