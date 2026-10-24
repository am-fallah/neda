package scheduler_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/scheduler"
)

type midpointRandom struct{}

func (midpointRandom) Int64N(n int64) int64 { return n / 2 }

func TestBackoff(t *testing.T) {
	now := schedulerNow
	interval := 300 * time.Second

	success := provider.Classification{Category: provider.CategorySuccess}
	infrastructure := provider.Classification{
		Category:           provider.CategoryInfrastructure,
		Kind:               provider.ErrTimeout,
		CountsTowardOutage: true,
		TargetBackoff:      true,
	}
	configuration := provider.Classification{
		Category: provider.CategoryConfiguration,
		Kind:     provider.ErrConfiguration,
	}
	route := provider.Classification{
		Category: provider.CategoryRoute,
		Kind:     provider.ErrRouteNotFound,
	}
	request := provider.Classification{
		Category: provider.CategoryRequest,
		Kind:     provider.ErrHTTPStatus,
	}
	abandoned := provider.Classification{
		Category: provider.CategoryAbandoned,
		Kind:     provider.ErrCanceled,
	}

	tests := []struct {
		name         string
		now          time.Time
		interval     time.Duration
		failures     int
		class        provider.Classification
		random       scheduler.Random
		wantApply    bool
		wantFailures int
		wantDelay    time.Duration
		wantErr      bool
	}{
		{
			name: "success resets counter and uses normal interval",
			now:  now, interval: interval, failures: 4,
			class: success, random: midpointRandom{},
			wantApply: true, wantFailures: 0, wantDelay: interval,
		},
		{
			name: "config error keeps counter and uses normal interval",
			now:  now, interval: interval, failures: 2,
			class: configuration, random: midpointRandom{},
			wantApply: true, wantFailures: 2, wantDelay: interval,
		},
		{
			name: "route error keeps counter and uses normal interval",
			now:  now, interval: interval, failures: 2,
			class: route, random: midpointRandom{},
			wantApply: true, wantFailures: 2, wantDelay: interval,
		},
		{
			name: "ordinary 4xx keeps counter and uses normal interval",
			now:  now, interval: interval, failures: 2,
			class: request, random: midpointRandom{},
			wantApply: true, wantFailures: 2, wantDelay: interval,
		},
		{
			name: "abandoned does not update target scheduling",
			now:  now, interval: interval, failures: 2,
			class: abandoned, random: midpointRandom{},
			wantApply: false,
		},
		{
			name: "first infrastructure failure is two intervals",
			now:  now, interval: interval, failures: 0,
			class: infrastructure, random: midpointRandom{},
			wantApply: true, wantFailures: 1, wantDelay: 2 * interval,
		},
		{
			name: "second infrastructure failure is four intervals",
			now:  now, interval: interval, failures: 1,
			class: infrastructure, random: midpointRandom{},
			wantApply: true, wantFailures: 2, wantDelay: 4 * interval,
		},
		{
			name: "third infrastructure failure caps at thirty minutes",
			now:  now, interval: interval, failures: 2,
			class: infrastructure, random: midpointRandom{},
			wantApply: true, wantFailures: 3, wantDelay: 30 * time.Minute,
		},
		{
			name: "large failure count stays capped",
			now:  now, interval: interval, failures: 100,
			class: infrastructure, random: midpointRandom{},
			wantApply: true, wantFailures: 101, wantDelay: 30 * time.Minute,
		},
		{
			name: "short interval first failure is two intervals",
			now:  now, interval: 60 * time.Second, failures: 0,
			class: infrastructure, random: midpointRandom{},
			wantApply: true, wantFailures: 1, wantDelay: 120 * time.Second,
		},
		{
			name: "short interval cap is thirty minutes",
			now:  now, interval: 60 * time.Second, failures: 5,
			class: infrastructure, random: midpointRandom{},
			wantApply: true, wantFailures: 6, wantDelay: 30 * time.Minute,
		},
		{
			name: "long interval first failure is two hours",
			now:  now, interval: 3600 * time.Second, failures: 0,
			class: infrastructure, random: midpointRandom{},
			wantApply: true, wantFailures: 1, wantDelay: 2 * time.Hour,
		},
		{
			name: "long interval cap is four hours",
			now:  now, interval: 3600 * time.Second, failures: 1,
			class: infrastructure, random: midpointRandom{},
			wantApply: true, wantFailures: 2, wantDelay: 4 * time.Hour,
		},
		{
			name: "long interval stays capped",
			now:  now, interval: 3600 * time.Second, failures: 2,
			class: infrastructure, random: midpointRandom{},
			wantApply: true, wantFailures: 3, wantDelay: 4 * time.Hour,
		},
		{
			name: "retry-after raises the final delay",
			now:  now, interval: interval, failures: 0,
			class: provider.Classification{
				Category: provider.CategoryInfrastructure, Kind: provider.ErrRateLimited,
				RetryAfter: 45 * time.Minute, CountsTowardOutage: true, TargetBackoff: true,
			},
			random:    midpointRandom{},
			wantApply: true, wantFailures: 1, wantDelay: 45 * time.Minute,
		},
		{
			name: "retry-after below exponential delay is ignored",
			now:  now, interval: interval, failures: 0,
			class: provider.Classification{
				Category: provider.CategoryInfrastructure, Kind: provider.ErrRateLimited,
				RetryAfter: 2 * time.Second, CountsTowardOutage: true, TargetBackoff: true,
			},
			random:    midpointRandom{},
			wantApply: true, wantFailures: 1, wantDelay: 10 * time.Minute,
		},
		{
			name: "negative jitter is applied after capping",
			now:  now, interval: interval, failures: 0,
			class: infrastructure, random: &scriptedRandom{values: []int64{0}},
			wantApply: true, wantFailures: 1, wantDelay: 9 * time.Minute,
		},
		{
			name: "positive jitter is applied after capping",
			now:  now, interval: interval, failures: 0,
			class: infrastructure, random: &scriptedRandom{values: []int64{int64(120 * time.Second)}},
			wantApply: true, wantFailures: 1, wantDelay: 11 * time.Minute,
		},
		{
			name: "jitter may exceed the cap",
			now:  now, interval: interval, failures: 10,
			class: infrastructure, random: &scriptedRandom{values: []int64{int64(360 * time.Second)}},
			wantApply: true, wantFailures: 11, wantDelay: 33 * time.Minute,
		},
		{
			name: "retry-after is a lower bound after jitter",
			now:  now, interval: interval, failures: 0,
			class: provider.Classification{
				Category: provider.CategoryInfrastructure, Kind: provider.ErrRateLimited,
				RetryAfter: 12 * time.Minute, CountsTowardOutage: true, TargetBackoff: true,
			},
			random:    &scriptedRandom{values: []int64{0}},
			wantApply: true, wantFailures: 1, wantDelay: 12 * time.Minute,
		},
		{
			name: "zero time is rejected",
			now:  time.Time{}, interval: interval, failures: 0,
			class: success, random: midpointRandom{}, wantErr: true,
		},
		{
			name: "interval below product minimum is rejected",
			now:  now, interval: 59 * time.Second, failures: 0,
			class: success, random: midpointRandom{}, wantErr: true,
		},
		{
			name: "interval above product maximum is rejected",
			now:  now, interval: 3601 * time.Second, failures: 0,
			class: success, random: midpointRandom{}, wantErr: true,
		},
		{
			name: "negative failure count is rejected",
			now:  now, interval: interval, failures: -1,
			class: success, random: midpointRandom{}, wantErr: true,
		},
		{
			name: "empty classification category is rejected",
			now:  now, interval: interval, failures: 0,
			class: provider.Classification{}, random: midpointRandom{}, wantErr: true,
		},
		{
			name: "failure counter overflow is rejected",
			now:  now, interval: interval, failures: math.MaxInt,
			class: infrastructure, random: midpointRandom{}, wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := scheduler.DecideTargetSchedule(test.now, test.interval, test.failures, test.class, test.random)
			if test.wantErr {
				if err == nil {
					t.Fatalf("DecideTargetSchedule succeeded: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecideTargetSchedule: %v", err)
			}
			if got.Apply != test.wantApply {
				t.Fatalf("Apply = %v, want %v", got.Apply, test.wantApply)
			}
			if !test.wantApply {
				return
			}
			if got.ConsecutiveFailures != test.wantFailures {
				t.Fatalf("ConsecutiveFailures = %d, want %d", got.ConsecutiveFailures, test.wantFailures)
			}
			if want := test.now.Add(test.wantDelay); !got.NextDueAt.Equal(want) {
				t.Fatalf("NextDueAt = %v, want %v", got.NextDueAt, want)
			}
		})
	}
}

func TestBackoffConsumesClassifyContract(t *testing.T) {
	now := schedulerNow
	interval := 300 * time.Second

	success, err := scheduler.DecideTargetSchedule(now, interval, 3,
		provider.Classify(context.Background(), nil), midpointRandom{})
	if err != nil || !success.Apply || success.ConsecutiveFailures != 0 {
		t.Fatalf("success decision = %+v, %v", success, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	abandoned, err := scheduler.DecideTargetSchedule(now, interval, 3,
		provider.Classify(canceled, nil), midpointRandom{})
	if err != nil || abandoned.Apply {
		t.Fatalf("abandoned decision = %+v, %v", abandoned, err)
	}

	timeout, err := scheduler.DecideTargetSchedule(now, interval, 0,
		provider.Classify(context.Background(), &provider.Error{Kind: provider.ErrTimeout}), midpointRandom{})
	if err != nil || !timeout.Apply || timeout.ConsecutiveFailures != 1 {
		t.Fatalf("timeout decision = %+v, %v", timeout, err)
	}

	notFound, err := scheduler.DecideTargetSchedule(now, interval, 2,
		provider.Classify(context.Background(), &provider.Error{Kind: provider.ErrHTTPStatus, StatusCode: 404}), midpointRandom{})
	if err != nil || !notFound.Apply || notFound.ConsecutiveFailures != 2 {
		t.Fatalf("404 decision = %+v, %v", notFound, err)
	}
}
