package main

import (
	"testing"

	"github.com/am-fallah/neda/internal/config"
	"github.com/am-fallah/neda/internal/provider/fake"
)

func TestProviderRegistryForConfig(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.Config
		wantFake   bool
		wantLookup bool
	}{
		{
			name:       "development fake enabled",
			cfg:        config.Config{Env: config.Development, FakeProviderEnabled: true},
			wantFake:   true,
			wantLookup: true,
		},
		{
			name:       "production fake ignored",
			cfg:        config.Config{Env: config.Production, FakeProviderEnabled: true},
			wantFake:   false,
			wantLookup: false,
		},
		{
			name:       "development fake disabled",
			cfg:        config.Config{Env: config.Development},
			wantFake:   false,
			wantLookup: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, fakeEnabled, err := providerRegistryForConfig(test.cfg)
			if err != nil {
				t.Fatalf("providerRegistryForConfig: %v", err)
			}
			if fakeEnabled != test.wantFake {
				t.Fatalf("fakeEnabled = %v, want %v", fakeEnabled, test.wantFake)
			}
			_, found := registry.Lookup(fake.ID)
			if found != test.wantLookup {
				t.Fatalf("fake lookup = %v, want %v", found, test.wantLookup)
			}
		})
	}
}
