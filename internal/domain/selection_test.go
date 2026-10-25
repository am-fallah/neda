package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/provider"
)

type optionAcceptingProvider struct{ id provider.ID }

func (p optionAcceptingProvider) Metadata() provider.Metadata {
	return provider.Metadata{
		ID: p.id, DisplayName: "Option accepting provider",
		Capabilities: provider.Capabilities{
			RequiresOriginID: true, RequiresDestinationID: true,
			RequiresOptionSelection: true, MaxPassengers: 9,
		},
	}
}

func (p optionAcceptingProvider) ValidateConfig(cfg provider.MonitorProviderConfig) error {
	if cfg.OriginID == "" || cfg.DestinationID == "" || cfg.OriginID == cfg.DestinationID {
		return errors.New("invalid route")
	}
	return nil
}

func (p optionAcceptingProvider) Check(context.Context, provider.CheckRequest) (provider.Result, error) {
	return provider.Result{}, nil
}

func optionSelection() provider.SelectedOption {
	departure := time.Date(2026, 11, 5, 11, 20, 0, 0, time.UTC)
	return provider.SelectedOption{
		JourneyKey: "id:train", OfferingKey: "id:class",
		JourneyNativeID: "train", OfferingNativeID: "class",
		TrainNumber: "682", Operator: "Raja",
		OriginName: "Yazd", DestinationName: "Mashhad",
		DepartureAt: &departure,
		WagonID:     "wagon", WagonName: "Wagon", ClassName: "Class",
	}
}

func selectionInput() domain.MonitorInput {
	return domain.MonitorInput{
		Label: "MrBilit selection", OriginName: "Yazd", DestinationName: "Mashhad",
		DepartureDate: "2026-11-05", Adults: 1,
		Providers: []domain.ProviderInput{{
			ProviderID: "selection", OriginID: "219", DestinationID: "191",
			BookingSlug: "yazd-mashhad",
			Selections:  []provider.SelectedOption{optionSelection()},
		}},
	}
}

func TestMonitorSelectionValidation(t *testing.T) {
	h := newServiceHarness(t)
	ctx := context.Background()

	created, err := h.svc.Create(ctx, selectionInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(created.Providers) != 1 || len(created.Providers[0].Selections) != 1 {
		t.Fatalf("created = %+v", created)
	}

	missing := selectionInput()
	missing.Providers[0].Selections = nil
	if _, err := h.svc.Create(ctx, missing); !errors.Is(err, domain.ErrMonitorInvalid) {
		t.Fatalf("missing selections error = %v", err)
	}

	unsupported := selectionInput()
	unsupported.Providers[0].ProviderID = "stub"
	if _, err := h.svc.Create(ctx, unsupported); !errors.Is(err, domain.ErrMonitorInvalid) {
		t.Fatalf("unsupported selections error = %v", err)
	}

	wrongDate := selectionInput()
	wrongDate.DepartureDate = "2026-11-06"
	if _, err := h.svc.Create(ctx, wrongDate); !errors.Is(err, domain.ErrMonitorInvalid) {
		t.Fatalf("mismatched selection date error = %v", err)
	}

	duplicate := selectionInput()
	duplicate.Providers[0].Selections = append(duplicate.Providers[0].Selections, optionSelection())
	if _, err := h.svc.Create(ctx, duplicate); !errors.Is(err, domain.ErrMonitorInvalid) {
		t.Fatalf("duplicate selections error = %v", err)
	}
}
