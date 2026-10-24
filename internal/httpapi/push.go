package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/push"
)

// PushService is the authenticated Push configuration/subscription surface
// owned by PSH-002/PSH-006. It never exposes endpoint or key material.
type PushService interface {
	Config(context.Context) (push.ConfigView, error)
	Register(context.Context, push.SubscriptionInput) (push.Subscription, error)
	List(context.Context) ([]push.Subscription, error)
	Disable(context.Context, string) error
}

// PushTester sends a test notification through the configured Push sender.
type PushTester interface {
	Test(context.Context, string) (push.TestOutcome, error)
}

type pushConfigResponse struct {
	Enabled           bool                       `json:"enabled"`
	Available         bool                       `json:"available"`
	SubjectConfigured bool                       `json:"subjectConfigured"`
	PublicKey         string                     `json:"publicKey,omitempty"`
	Subscriptions     []pushSubscriptionResponse `json:"subscriptions"`
}

type pushSubscriptionResponse struct {
	ID            string     `json:"id"`
	Label         string     `json:"label,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
	FailureCount  int        `json:"failureCount"`
}

type pushSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Label string `json:"label"`
}

type pushTestRequest struct {
	SubscriptionID string `json:"subscriptionId"`
}

type pushTestResponse struct {
	Attempted  int      `json:"attempted"`
	Sent       int      `json:"sent"`
	Failed     int      `json:"failed"`
	ErrorKinds []string `json:"errorKinds,omitempty"`
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/push")
	switch {
	case path == "/config":
		if !requireMethods(w, r, http.MethodGet) {
			return
		}
		s.getPushConfig(w, r)
	case path == "/subscriptions":
		if !requireMethods(w, r, http.MethodPost) {
			return
		}
		s.registerPushSubscription(w, r)
	case strings.HasPrefix(path, "/subscriptions/"):
		if !requireMethods(w, r, http.MethodDelete) {
			return
		}
		s.deletePushSubscription(w, r, strings.TrimPrefix(path, "/subscriptions/"))
	case path == "/test":
		if !requireMethods(w, r, http.MethodPost) {
			return
		}
		s.testPush(w, r)
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "not found")
	}
}

func (s *Server) getPushConfig(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeJSON(w, http.StatusOK, pushConfigResponse{
			Available:     false,
			Subscriptions: []pushSubscriptionResponse{},
		})
		return
	}
	view, err := s.push.Config(r.Context())
	if err != nil {
		s.writePushError(w, r, err)
		return
	}
	response := pushConfigResponse{
		Enabled:           view.Enabled,
		Available:         view.Available,
		SubjectConfigured: view.SubjectConfigured,
		PublicKey:         view.PublicKey,
		Subscriptions:     pushSubscriptionsDTO(view.Subscriptions),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) registerPushSubscription(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeError(w, r, http.StatusServiceUnavailable, "push_unavailable", "Web Push is not configured")
		return
	}
	var body pushSubscriptionRequest
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, r, err, "invalid subscription request")
		return
	}
	subscription, err := s.push.Register(r.Context(), push.SubscriptionInput{
		Endpoint: body.Endpoint,
		P256DH:   body.Keys.P256DH,
		Auth:     body.Keys.Auth,
		Label:    body.Label,
	})
	if err != nil {
		s.writePushError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, pushSubscriptionDTO(subscription))
}

func (s *Server) deletePushSubscription(w http.ResponseWriter, r *http.Request, id string) {
	if s.push == nil {
		writeError(w, r, http.StatusServiceUnavailable, "push_unavailable", "Web Push is not configured")
		return
	}
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") {
		writeError(w, r, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err := s.push.Disable(r.Context(), id); err != nil {
		s.writePushError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) testPush(w http.ResponseWriter, r *http.Request) {
	if s.pushTester == nil {
		writeError(w, r, http.StatusServiceUnavailable, "push_unavailable", "Web Push is not configured")
		return
	}
	var body pushTestRequest
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, r, err, "invalid push test request")
		return
	}
	outcome, err := s.pushTester.Test(r.Context(), body.SubscriptionID)
	if err != nil {
		s.writePushError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pushTestResponse{
		Attempted:  outcome.Attempted,
		Sent:       outcome.Sent,
		Failed:     outcome.Failed,
		ErrorKinds: outcome.ErrorKinds,
	})
}

func (s *Server) writePushError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *push.ValidationError
	switch {
	case errors.As(err, &validation):
		fields := map[string]string{}
		if validation.Field != "" {
			fields[validation.Field] = validation.Message
		}
		writeErrorWithFields(w, r, http.StatusBadRequest, "validation_error", "validation failed", fields)
	case errors.Is(err, push.ErrUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "push_unavailable", "Web Push is not configured")
	case errors.Is(err, push.ErrPushDisabled):
		writeError(w, r, http.StatusConflict, "push_disabled", "Web Push is disabled in settings")
	case errors.Is(err, push.ErrNoSubscriptions):
		writeError(w, r, http.StatusConflict, "no_subscriptions", "no active Push subscriptions")
	case errors.Is(err, push.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "subscription not found")
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func pushSubscriptionsDTO(subscriptions []push.Subscription) []pushSubscriptionResponse {
	response := make([]pushSubscriptionResponse, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		response = append(response, pushSubscriptionDTO(subscription))
	}
	return response
}

func pushSubscriptionDTO(subscription push.Subscription) pushSubscriptionResponse {
	return pushSubscriptionResponse{
		ID:            subscription.ID,
		Label:         subscription.Label,
		CreatedAt:     subscription.CreatedAt.UTC(),
		LastSuccessAt: subscription.LastSuccessAt,
		FailureCount:  subscription.FailureCount,
	}
}
