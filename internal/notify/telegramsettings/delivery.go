package telegramsettings

import (
	"context"
	"database/sql"
	"errors"

	"github.com/am-fallah/neda/internal/notify"
)

// DeliveryDestinations reports whether Telegram is configured for one outcome
// transaction. It performs only a short settings read and returns a single
// stable destination; the delivery worker decrypts credentials outside the
// transaction at send time. Missing or incomplete settings produce no delivery.
func (s *Settings) DeliveryDestinations(ctx context.Context, tx *sql.Tx) ([]notify.Delivery, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("telegramsettings: settings are not initialized")
	}
	if tx == nil {
		return nil, errors.New("telegramsettings: nil transaction")
	}
	rows, err := tx.QueryContext(ctx, `SELECT key FROM settings WHERE key IN (?, ?)`,
		TelegramBotTokenSetting, TelegramChatIDSetting)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		found++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if found != 2 {
		return nil, nil
	}
	return []notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "telegram"}}, nil
}
