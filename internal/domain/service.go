package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

// MonitorServiceConfig controls the monitor CRUD service. Zero values receive
// safe defaults; tests inject a clock, location, ID generator, and wake hook.
type MonitorServiceConfig struct {
	Now           func() time.Time
	Location      *time.Location
	NearTermDelay time.Duration // SCH-002 supplies jitter; zero schedules immediately
	NewID         func() (string, error)
	Wake          func()
}

// MonitorService owns monitor validation and CRUD orchestration. It does not
// implement scheduling or provider I/O.
type MonitorService struct {
	repo      MonitorRepository
	providers ProviderResolver
	now       func() time.Time
	loc       *time.Location
	delay     time.Duration
	newID     func() (string, error)
	wake      func()
}

// NewMonitorService validates dependencies and applies defaults.
func NewMonitorService(repo MonitorRepository, providers ProviderResolver, cfg MonitorServiceConfig) (*MonitorService, error) {
	if repo == nil {
		return nil, errors.New("domain: nil monitor repository")
	}
	if providers == nil {
		return nil, errors.New("domain: nil provider resolver")
	}
	if cfg.Location == nil {
		location, err := TehranLocation()
		if err != nil {
			return nil, err
		}
		cfg.Location = location
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewID == nil {
		cfg.NewID = randomMonitorID
	}
	if cfg.Wake == nil {
		cfg.Wake = func() {}
	}
	if cfg.NearTermDelay < 0 {
		return nil, errors.New("domain: near-term delay must not be negative")
	}
	return &MonitorService{
		repo:      repo,
		providers: providers,
		now:       cfg.Now,
		loc:       cfg.Location,
		delay:     cfg.NearTermDelay,
		newID:     cfg.NewID,
		wake:      cfg.Wake,
	}, nil
}

// Create validates and persists a new enabled monitor with active targets.
func (s *MonitorService) Create(ctx context.Context, input MonitorInput) (Monitor, error) {
	now := s.now().UTC()
	normalized, expiresAt, err := normalizeMonitorInput(input, now, s.loc, s.providers)
	if err != nil {
		return Monitor{}, err
	}
	id, err := s.newID()
	if err != nil {
		return Monitor{}, fmt.Errorf("domain: generate monitor id: %w", err)
	}
	scheduleAt := now.Add(s.delay)
	monitor := Monitor{
		ID:              id,
		Label:           normalized.Label,
		OriginName:      normalized.OriginName,
		DestinationName: normalized.DestinationName,
		DepartureDate:   normalized.DepartureDate,
		Adults:          normalized.Adults,
		Children:        normalized.Children,
		Infants:         normalized.Infants,
		Enabled:         true,
		ExpiresAt:       expiresAt,
		CreatedAt:       now,
		UpdatedAt:       now,
		Providers:       make([]MonitorProvider, 0, len(normalized.Providers)),
	}
	for _, candidate := range normalized.Providers {
		due := scheduleAt
		monitor.Providers = append(monitor.Providers, MonitorProvider{
			MonitorID:     id,
			ProviderID:    candidate.ProviderID,
			Active:        true,
			OriginID:      candidate.OriginID,
			DestinationID: candidate.DestinationID,
			BookingSlug:   candidate.BookingSlug,
			GenderCode:    candidate.GenderCode,
			ConfigVersion: 1,
			NextDueAt:     &due,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}

	created, err := s.repo.CreateMonitor(ctx, monitor)
	if err != nil {
		return Monitor{}, err
	}
	s.wake()
	return created, nil
}

// Update validates the edit and applies material-change/config-version rules
// atomically in the repository. The wake hook runs only after the repository
// returns (after commit) and only for material edits.
func (s *MonitorService) Update(ctx context.Context, id string, input MonitorInput) (Monitor, error) {
	now := s.now().UTC()
	normalized, _, err := normalizeMonitorInput(input, now, s.loc, s.providers)
	if err != nil {
		return Monitor{}, err
	}
	result, err := s.repo.UpdateMonitor(ctx, id, normalized, now, now.Add(s.delay), s.loc)
	if err != nil {
		return Monitor{}, err
	}
	if result.Material {
		s.wake()
	}
	return result.Monitor, nil
}

// Get returns one monitor with all provider rows.
func (s *MonitorService) Get(ctx context.Context, id string) (Monitor, error) {
	return s.repo.GetMonitor(ctx, id)
}

// List returns monitors with all provider rows.
func (s *MonitorService) List(ctx context.Context) ([]Monitor, error) {
	return s.repo.ListMonitors(ctx)
}

// Pause disables polling for the monitor. It is idempotent and does not bump
// config versions or close episodes.
func (s *MonitorService) Pause(ctx context.Context, id string) (Monitor, error) {
	monitor, err := s.repo.PauseMonitor(ctx, id, s.now().UTC())
	if err != nil {
		return Monitor{}, err
	}
	s.wake()
	return monitor, nil
}

// Resume re-enables a non-expired monitor and schedules active targets
// near-term. Expired monitors must first receive a future departure date.
func (s *MonitorService) Resume(ctx context.Context, id string) (Monitor, error) {
	now := s.now().UTC()
	monitor, err := s.repo.ResumeMonitor(ctx, id, now, now.Add(s.delay))
	if err != nil {
		return Monitor{}, err
	}
	s.wake()
	return monitor, nil
}

// Delete hard-deletes a monitor and its cascade-owned history.
func (s *MonitorService) Delete(ctx context.Context, id string) error {
	if err := s.repo.DeleteMonitor(ctx, id); err != nil {
		return err
	}
	s.wake()
	return nil
}

func randomMonitorID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

var _ ProviderResolver = (*provider.Registry)(nil)
