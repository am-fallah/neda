package telegramsettings_test

import (
	"context"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	"github.com/am-fallah/neda/internal/store"
)

func TestDeliveryDestinations(t *testing.T) {
	ctx := context.Background()
	database := newSettingsTestDB(t)
	st := store.New(database)
	settings, err := telegramsettings.New(st, settingsTestKey(t, 6), func() time.Time { return settingsTestNow })
	if err != nil {
		t.Fatal(err)
	}

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err := settings.DeliveryDestinations(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if len(deliveries) != 0 {
		t.Fatalf("unconfigured deliveries = %+v", deliveries)
	}

	if err := settings.Save(ctx, settingsToken, settingsChatID); err != nil {
		t.Fatal(err)
	}
	tx, err = database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err = settings.DeliveryDestinations(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if len(deliveries) != 1 || deliveries[0].Channel != notify.ChannelTelegram || deliveries[0].TargetKey != "telegram" {
		t.Fatalf("configured deliveries = %+v", deliveries)
	}

	if err := st.DeleteSetting(ctx, telegramsettings.TelegramChatIDSetting); err != nil {
		t.Fatal(err)
	}
	tx, err = database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err = settings.DeliveryDestinations(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if len(deliveries) != 0 {
		t.Fatalf("incomplete deliveries = %+v", deliveries)
	}

	if _, err := settings.DeliveryDestinations(ctx, nil); err == nil {
		t.Fatal("nil transaction accepted")
	}
}
