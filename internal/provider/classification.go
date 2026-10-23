package provider

import (
	"context"
	"errors"
	"time"
)

// Category is the stable classification consumed by scheduler and health. It
// is intentionally separate from ErrorKind: Kind describes the wire/error
// category, Category describes what the core is allowed to do with it.
type Category string

const (
	CategorySuccess        Category = "success"
	CategoryInfrastructure Category = "infrastructure"
	CategoryConfiguration  Category = "configuration"
	CategoryRoute          Category = "route"
	CategoryRequest        Category = "request"
	CategoryAbandoned      Category = "abandoned"
)

// Classification is a pure error classification. It never mutates provider or
// database state. Scheduler and health must consume these fields rather than
// redefining ErrorKind semantics; SCH-006 owns the health transition table.
type Classification struct {
	Category           Category
	Kind               ErrorKind
	StatusCode         int
	RetryAfter         time.Duration
	CountsTowardOutage bool
	TargetBackoff      bool
}

// IsSuccess reports a successful provider outcome.
func (c Classification) IsSuccess() bool {
	return c.Category == CategorySuccess
}

// IsInfrastructure reports an applicable infrastructure failure.
func (c Classification) IsInfrastructure() bool {
	return c.Category == CategoryInfrastructure
}

// IsAbandoned reports caller cancellation/shutdown.
func (c Classification) IsAbandoned() bool {
	return c.Category == CategoryAbandoned
}

// Classify maps a provider result/error and the caller context to the shared
// scheduler/health contract in Architecture §10.3.
//
// Context cancellation wins over a provider error: caller cancellation or
// shutdown is abandoned, and a deadline is an infrastructure timeout. A
// provider-side cancellation without caller cancellation is an invalid
// response and counts as an infrastructure failure.
func Classify(ctx context.Context, err error) Classification {
	if ctx != nil {
		switch ctx.Err() {
		case context.Canceled:
			return Classification{Category: CategoryAbandoned, Kind: ErrCanceled}
		case context.DeadlineExceeded:
			return infrastructure(ErrTimeout, 0, 0)
		}
	}
	if err == nil {
		return Classification{Category: CategorySuccess}
	}

	var providerErr *Error
	if errors.As(err, &providerErr) && providerErr != nil {
		return classifyProviderError(providerErr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return infrastructure(ErrTimeout, 0, 0)
	}
	if errors.Is(err, context.Canceled) {
		// The caller context is not canceled, so this is a provider-side
		// cancellation and must not be treated as abandonment.
		return infrastructure(ErrInvalidResponse, 0, 0)
	}
	return infrastructure(ErrInvalidResponse, 0, 0)
}

func classifyProviderError(providerErr *Error) Classification {
	status := providerErr.StatusCode
	retryAfter := providerErr.RetryAfter
	if retryAfter < 0 {
		retryAfter = 0
	}

	switch providerErr.Kind {
	case ErrTimeout:
		return infrastructure(ErrTimeout, status, retryAfter)
	case ErrNetwork:
		return infrastructure(ErrNetwork, status, retryAfter)
	case ErrRateLimited:
		return infrastructure(ErrRateLimited, status, retryAfter)
	case ErrInvalidResponse:
		return infrastructure(ErrInvalidResponse, status, retryAfter)
	case ErrRouteNotFound:
		return nonInfrastructure(CategoryRoute, ErrRouteNotFound, status, retryAfter)
	case ErrConfiguration:
		return nonInfrastructure(CategoryConfiguration, ErrConfiguration, status, retryAfter)
	case ErrCanceled:
		// Classify checks the caller context first, so reaching this branch
		// means the adapter reported cancellation without caller cancellation.
		return infrastructure(ErrInvalidResponse, status, retryAfter)
	case ErrHTTPStatus:
		switch {
		case status == 429:
			return infrastructure(ErrRateLimited, status, retryAfter)
		case status >= 500 && status <= 599:
			return infrastructure(ErrHTTPStatus, status, retryAfter)
		case status >= 400 && status <= 499:
			return nonInfrastructure(CategoryRequest, ErrHTTPStatus, status, retryAfter)
		default:
			return infrastructure(ErrInvalidResponse, status, retryAfter)
		}
	default:
		return infrastructure(ErrInvalidResponse, status, retryAfter)
	}
}

func infrastructure(kind ErrorKind, status int, retryAfter time.Duration) Classification {
	return Classification{
		Category:           CategoryInfrastructure,
		Kind:               kind,
		StatusCode:         status,
		RetryAfter:         retryAfter,
		CountsTowardOutage: true,
		TargetBackoff:      true,
	}
}

func nonInfrastructure(category Category, kind ErrorKind, status int, retryAfter time.Duration) Classification {
	return Classification{
		Category:   category,
		Kind:       kind,
		StatusCode: status,
		RetryAfter: retryAfter,
	}
}
