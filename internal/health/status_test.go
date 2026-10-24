package health_test

import (
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/health"
)

func TestStaleThreshold(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{"zero uses floor", 0, 15 * time.Minute},
		{"negative uses floor", -time.Minute, 15 * time.Minute},
		{"short interval uses floor", 4 * time.Minute, 15 * time.Minute},
		{"exactly five minutes uses floor", 5 * time.Minute, 15 * time.Minute},
		{"longer interval uses three times", 10 * time.Minute, 30 * time.Minute},
		{"one hour uses three times", time.Hour, 3 * time.Hour},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := health.StaleThreshold(test.interval); got != test.want {
				t.Fatalf("StaleThreshold(%s) = %s, want %s", test.interval, got, test.want)
			}
		})
	}
}

func TestIsStale(t *testing.T) {
	now := testNow
	recent := now.Add(-10 * time.Minute)
	old := now.Add(-16 * time.Minute)
	exact := now.Add(-15 * time.Minute)
	veryOld := now.Add(-7 * time.Hour)

	tests := []struct {
		name        string
		lastSuccess *time.Time
		openEpisode bool
		now         time.Time
		interval    time.Duration
		want        bool
	}{
		{"open episode is stale", &recent, true, now, 5 * time.Minute, true},
		{"nil last success is not stale here", nil, false, now, 5 * time.Minute, false},
		{"zero now is not stale", &old, false, time.Time{}, 5 * time.Minute, false},
		{"recent success is not stale", &recent, false, now, 5 * time.Minute, false},
		{"old success is stale", &old, false, now, 5 * time.Minute, true},
		{"exact threshold is not older", &exact, false, now, 5 * time.Minute, false},
		{"long interval keeps recent success fresh", &old, false, now, 2 * time.Hour, false},
		{"long interval old success eventually stale", &veryOld, false, now, 2 * time.Hour, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := health.IsStale(test.lastSuccess, test.openEpisode, test.now, test.interval); got != test.want {
				t.Fatalf("IsStale() = %v, want %v", got, test.want)
			}
		})
	}
}
