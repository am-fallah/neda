package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/logging"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
	appsettings "github.com/am-fallah/neda/internal/settings"
)

type settingsResponse struct {
	PollIntervalSeconds int                      `json:"pollIntervalSeconds"`
	PushEnabled         bool                     `json:"pushEnabled"`
	Telegram            telegramSettingsResponse `json:"telegram"`
}

type telegramSettingsResponse struct {
	Available         bool       `json:"available"`
	Configured        bool       `json:"configured"`
	Readable          bool       `json:"readable"`
	ChatLabel         string     `json:"chatLabel,omitempty"`
	SettingsErrorKind string     `json:"settingsErrorKind,omitempty"`
	LastTestAt        *time.Time `json:"lastTestAt"`
	LastTestOK        *bool      `json:"lastTestOk"`
	LastErrorKind     string     `json:"lastErrorKind,omitempty"`
}

type settingsUpdateRequest struct {
	PollIntervalSeconds *int  `json:"pollIntervalSeconds"`
	PushEnabled         *bool `json:"pushEnabled"`
}

type telegramConfigRequest struct {
	Token  string `json:"token"`
	ChatID string `json:"chatId"`
	Clear  bool   `json:"clear"`
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		writeError(w, r, http.StatusServiceUnavailable, "settings_unavailable", "settings are not available")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/settings")
	switch path {
	case "":
		switch r.Method {
		case http.MethodGet:
			s.getSettings(w, r)
		case http.MethodPut:
			s.updateSettings(w, r)
		default:
			requireMethods(w, r, http.MethodGet, http.MethodPut)
		}
	case "/telegram":
		if !requireMethods(w, r, http.MethodPut) {
			return
		}
		s.updateTelegramSettings(w, r)
	case "/telegram/test":
		if !requireMethods(w, r, http.MethodPost) {
			return
		}
		s.testTelegramSettings(w, r)
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "not found")
	}
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	view, err := s.settings.View(r.Context())
	if err != nil {
		s.writeSettingsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO(view))
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsUpdateRequest
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, r, err, "invalid settings request")
		return
	}
	view, err := s.settings.Update(r.Context(), appsettings.Update{
		PollIntervalSeconds: body.PollIntervalSeconds,
		PushEnabled:         body.PushEnabled,
	})
	if err != nil {
		s.writeSettingsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO(view))
}

func (s *Server) updateTelegramSettings(w http.ResponseWriter, r *http.Request) {
	var body telegramConfigRequest
	if err := decodeJSON(r, &body); err != nil {
		writeDecodeError(w, r, err, "invalid Telegram settings request")
		return
	}
	var (
		view appsettings.View
		err  error
	)
	if body.Clear {
		if strings.TrimSpace(body.Token) != "" || strings.TrimSpace(body.ChatID) != "" {
			writeErrorWithFields(w, r, http.StatusBadRequest, "validation_error", "validation failed",
				map[string]string{"clear": "must not be combined with token or chatId"})
			return
		}
		view, err = s.settings.ClearTelegram(r.Context())
	} else {
		view, err = s.settings.SaveTelegram(r.Context(), body.Token, body.ChatID)
	}
	if err != nil {
		s.writeSettingsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO(view))
}

func (s *Server) testTelegramSettings(w http.ResponseWriter, r *http.Request) {
	result, err := s.settings.TestTelegram(r.Context())
	if err != nil {
		s.writeSettingsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsDTO(result.View))
}

func settingsDTO(view appsettings.View) settingsResponse {
	return settingsResponse{
		PollIntervalSeconds: view.PollIntervalSeconds,
		PushEnabled:         view.PushEnabled,
		Telegram: telegramSettingsResponse{
			Available:         view.Telegram.Available,
			Configured:        view.Telegram.Configured,
			Readable:          view.Telegram.Readable,
			ChatLabel:         view.Telegram.ChatLabel,
			SettingsErrorKind: view.Telegram.SettingsErrorKind,
			LastTestAt:        utcOptional(view.Telegram.LastTestAt),
			LastTestOK:        view.Telegram.LastTestOK,
			LastErrorKind:     view.Telegram.LastErrorKind,
		},
	}
}

func (s *Server) writeSettingsError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *appsettings.ValidationError
	switch {
	case errors.As(err, &validation):
		fields := map[string]string{}
		if validation.Field != "" {
			fields[validation.Field] = validation.Message
		}
		if len(fields) == 0 {
			writeError(w, r, http.StatusBadRequest, "validation_error", "validation failed")
			return
		}
		writeErrorWithFields(w, r, http.StatusBadRequest, "validation_error", "validation failed", fields)
	case errors.Is(err, appsettings.ErrTelegramUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "telegram_settings_unavailable", "Telegram settings are not available")
	case errors.Is(err, telegramsettings.ErrTelegramNotConfigured):
		writeError(w, r, http.StatusConflict, "telegram_not_configured", "Telegram is not configured")
	case errors.Is(err, telegramsettings.ErrTelegramSettingsUnreadable):
		writeError(w, r, http.StatusConflict, "telegram_settings_unreadable", "Telegram settings cannot be read")
	default:
		s.logger.Error("settings request failed",
			"request_id", logging.RequestID(r.Context()),
			"error", err,
		)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
