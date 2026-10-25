package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/am-fallah/neda/internal/config"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/provider/fake"
)

func TestProviderRegistryForConfig(t *testing.T) {
	tests := []struct {
		name           string
		cfg            config.Config
		wantFake       bool
		wantFakeLookup bool
		wantMrBilit    bool
		wantMrBilitUse bool
	}{
		{
			name:           "development fake enabled",
			cfg:            config.Config{Env: config.Development, FakeProviderEnabled: true},
			wantFake:       true,
			wantFakeLookup: true,
			wantMrBilit:    true,
			wantMrBilitUse: true,
		},
		{
			name:           "production fake ignored",
			cfg:            config.Config{Env: config.Production, FakeProviderEnabled: true},
			wantFake:       false,
			wantFakeLookup: false,
			wantMrBilit:    false,
			wantMrBilitUse: false,
		},
		{
			name:           "development fake disabled",
			cfg:            config.Config{Env: config.Development},
			wantFake:       false,
			wantFakeLookup: false,
			wantMrBilit:    true,
			wantMrBilitUse: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, flags, err := providerRegistryForConfig(test.cfg)
			if err != nil {
				t.Fatalf("providerRegistryForConfig: %v", err)
			}
			if flags.FakeEnabled != test.wantFake {
				t.Fatalf("fakeEnabled = %v, want %v", flags.FakeEnabled, test.wantFake)
			}
			if flags.MrBilitEnabled != test.wantMrBilit {
				t.Fatalf("mrbilitEnabled = %v, want %v", flags.MrBilitEnabled, test.wantMrBilit)
			}
			_, found := registry.Lookup(fake.ID)
			if found != test.wantFakeLookup {
				t.Fatalf("fake lookup = %v, want %v", found, test.wantFakeLookup)
			}
			_, found = registry.Lookup("mrbilit")
			if found != test.wantMrBilitUse {
				t.Fatalf("mrbilit lookup = %v, want %v", found, test.wantMrBilitUse)
			}
		})
	}
}

func TestComposeDeliverySources(t *testing.T) {
	ctx := context.Background()
	if source := composeDeliverySources(nil, nil); source != nil {
		t.Fatal("nil-only composition should be nil")
	}
	telegram := func(context.Context, *sql.Tx) ([]notify.Delivery, error) {
		return []notify.Delivery{{Channel: notify.ChannelTelegram, TargetKey: "telegram"}}, nil
	}
	push := func(context.Context, *sql.Tx) ([]notify.Delivery, error) {
		return []notify.Delivery{{Channel: notify.ChannelPush, TargetKey: "push:device"}}, nil
	}
	combined := composeDeliverySources(telegram, nil, push)
	deliveries, err := combined(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 2 || deliveries[0].Channel != notify.ChannelTelegram || deliveries[1].Channel != notify.ChannelPush {
		t.Fatalf("deliveries = %+v", deliveries)
	}

	sentinel := errors.New("source failed")
	failing := func(context.Context, *sql.Tx) ([]notify.Delivery, error) { return nil, sentinel }
	if _, err := composeDeliverySources(telegram, failing)(ctx, nil); !errors.Is(err, sentinel) {
		t.Fatalf("source error = %v", err)
	}
}
