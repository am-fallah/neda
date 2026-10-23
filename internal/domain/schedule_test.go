package domain_test

import (
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
)

func TestValidatePollIntervalBounds(t *testing.T) {
	if err := domain.ValidatePollInterval(domain.MinPollInterval); err != nil {
		t.Fatalf("min interval rejected: %v", err)
	}
	if err := domain.ValidatePollInterval(domain.MaxPollInterval); err != nil {
		t.Fatalf("max interval rejected: %v", err)
	}
	for _, interval := range []time.Duration{
		domain.MinPollInterval - time.Second,
		domain.MaxPollInterval + time.Second,
		0,
		-time.Second,
	} {
		if err := domain.ValidatePollInterval(interval); err == nil {
			t.Fatalf("ValidatePollInterval(%v) succeeded", interval)
		}
	}
}
