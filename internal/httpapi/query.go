package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/am-fallah/neda/internal/logging"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/store"
)

type queryError struct {
	Field   string
	Message string
}

func (e *queryError) Error() string {
	if e == nil {
		return "invalid query"
	}
	return e.Field + ": " + e.Message
}

func queryLimit(r *http.Request, max int) (int, *queryError) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > max {
		return 0, &queryError{Field: "limit", Message: "must be between 1 and " + strconv.Itoa(max)}
	}
	return value, nil
}

func queryEventKind(r *http.Request) (notify.EventKind, *queryError) {
	raw := strings.TrimSpace(r.URL.Query().Get("kind"))
	if raw == "" {
		return "", nil
	}
	kind := notify.EventKind(raw)
	switch kind {
	case notify.KindAvailability, notify.KindProviderDegraded, notify.KindProviderRecovered:
		return kind, nil
	default:
		return "", &queryError{Field: "kind", Message: "must be availability, provider_degraded, or provider_recovered"}
	}
}

func queryChannel(r *http.Request) (notify.Channel, *queryError) {
	raw := strings.TrimSpace(r.URL.Query().Get("channel"))
	if raw == "" {
		return "", nil
	}
	channel := notify.Channel(raw)
	switch channel {
	case notify.ChannelTelegram, notify.ChannelPush:
		return channel, nil
	default:
		return "", &queryError{Field: "channel", Message: "must be telegram or push"}
	}
}

func queryDeliveryStatus(r *http.Request) (store.DeliveryStatus, *queryError) {
	raw := strings.TrimSpace(r.URL.Query().Get("status"))
	if raw == "" {
		return "", nil
	}
	status := store.DeliveryStatus(raw)
	if !status.Valid() {
		return "", &queryError{
			Field:   "status",
			Message: "must be pending, sending, sent, retry_scheduled, failed, or skipped",
		}
	}
	return status, nil
}

func writeQueryError(w http.ResponseWriter, r *http.Request, err *queryError) {
	fields := map[string]string{}
	if err.Field != "" {
		fields[err.Field] = err.Message
	}
	if len(fields) == 0 {
		writeError(w, r, http.StatusBadRequest, "validation_error", "validation failed")
		return
	}
	writeErrorWithFields(w, r, http.StatusBadRequest, "validation_error", "validation failed", fields)
}

func (s *Server) writeReadModelError(w http.ResponseWriter, r *http.Request, err error, notFoundMessage string) {
	switch {
	case errors.Is(err, store.ErrInvalidCursor):
		writeError(w, r, http.StatusBadRequest, "invalid_cursor", "invalid pagination cursor")
	case errors.Is(err, store.ErrInvalidFilter):
		writeError(w, r, http.StatusBadRequest, "validation_error", "invalid query")
	case errors.Is(err, store.ErrNotFound):
		if notFoundMessage == "" {
			notFoundMessage = "not found"
		}
		writeError(w, r, http.StatusNotFound, "not_found", notFoundMessage)
	default:
		s.logger.Error("read model request failed",
			"request_id", logging.RequestID(r.Context()),
			"error", err,
		)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
