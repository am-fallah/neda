package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/notify"
	"github.com/am-fallah/neda/internal/notify/telegramsettings"
)

const (
	settingsTestToken  = "123456:test-token_ABC-def"
	settingsTestChatID = "-1001234567890"
)

type settingsPayload struct {
	PollIntervalSeconds int                     `json:"pollIntervalSeconds"`
	PushEnabled         bool                    `json:"pushEnabled"`
	Telegram            telegramSettingsPayload `json:"telegram"`
}

type telegramSettingsPayload struct {
	Available         bool       `json:"available"`
	Configured        bool       `json:"configured"`
	Readable          bool       `json:"readable"`
	ChatLabel         string     `json:"chatLabel"`
	SettingsErrorKind string     `json:"settingsErrorKind"`
	LastTestAt        *time.Time `json:"lastTestAt"`
	LastTestOK        *bool      `json:"lastTestOk"`
	LastErrorKind     string     `json:"lastErrorKind"`
}

func decodeSettings(t *testing.T, body []byte) settingsPayload {
	t.Helper()
	var payload settingsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode settings: %v (body=%s)", err, body)
	}
	return payload
}

func settingsUpdateBody(poll *int, push *bool) string {
	payload := map[string]any{}
	if poll != nil {
		payload["pollIntervalSeconds"] = *poll
	}
	if push != nil {
		payload["pushEnabled"] = *push
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func telegramConfigBody(token, chatID string, clear bool) string {
	encoded, err := json.Marshal(map[string]any{
		"token": token, "chatId": chatID, "clear": clear,
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestSettingsGetUpdateAndValidation(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)

	unauthenticated := fixture.request(t, http.MethodGet, "/api/v1/settings", "", "", "", "")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated settings status = %d", unauthenticated.Code)
	}

	recorder := fixture.request(t, http.MethodGet, "/api/v1/settings", "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET settings status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	payload := decodeSettings(t, recorder.Body.Bytes())
	if payload.PollIntervalSeconds != 300 || payload.PushEnabled {
		t.Fatalf("default settings = %+v", payload)
	}
	if !payload.Telegram.Available || payload.Telegram.Configured || payload.Telegram.Readable {
		t.Fatalf("default telegram settings = %+v", payload.Telegram)
	}
	if payload.Telegram.LastTestAt != nil || payload.Telegram.LastTestOK != nil {
		t.Fatalf("default test metadata = %+v", payload.Telegram)
	}

	noCSRF := fixture.request(t, http.MethodPut, "/api/v1/settings", settingsUpdateBody(intPtr(120), boolPtr(true)), session, "", authOrigin)
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("settings without CSRF status = %d", noCSRF.Code)
	}
	poll := 120
	push := true
	updated := fixture.request(t, http.MethodPut, "/api/v1/settings", settingsUpdateBody(&poll, &push), session, csrf, authOrigin)
	if updated.Code != http.StatusOK {
		t.Fatalf("PUT settings status = %d, body=%s", updated.Code, updated.Body.String())
	}
	updatedPayload := decodeSettings(t, updated.Body.Bytes())
	if updatedPayload.PollIntervalSeconds != 120 || !updatedPayload.PushEnabled {
		t.Fatalf("updated settings = %+v", updatedPayload)
	}
	reloaded := fixture.request(t, http.MethodGet, "/api/v1/settings", "", session, "", "")
	if reloaded.Code != http.StatusOK {
		t.Fatalf("reloaded settings status = %d", reloaded.Code)
	}
	if got := decodeSettings(t, reloaded.Body.Bytes()); got.PollIntervalSeconds != 120 || !got.PushEnabled {
		t.Fatalf("reloaded settings = %+v", got)
	}

	invalid := 30
	badInterval := fixture.request(t, http.MethodPut, "/api/v1/settings", settingsUpdateBody(&invalid, nil), session, csrf, authOrigin)
	if badInterval.Code != http.StatusBadRequest {
		t.Fatalf("invalid interval status = %d, body=%s", badInterval.Code, badInterval.Body.String())
	}
	var errorBody errorPayload
	if err := json.Unmarshal(badInterval.Body.Bytes(), &errorBody); err != nil {
		t.Fatalf("decode interval error: %v", err)
	}
	if errorBody.Error.Code != "validation_error" || errorBody.Error.Fields["pollIntervalSeconds"] == "" {
		t.Fatalf("invalid interval error = %+v", errorBody)
	}

	empty := fixture.request(t, http.MethodPut, "/api/v1/settings", "{}", session, csrf, authOrigin)
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty settings update status = %d, body=%s", empty.Code, empty.Body.String())
	}
	unknown := fixture.request(t, http.MethodPut, "/api/v1/settings", `{"bogus":true}`, session, csrf, authOrigin)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown settings field status = %d", unknown.Code)
	}
	method := fixture.request(t, http.MethodPost, "/api/v1/settings", "{}", session, csrf, authOrigin)
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST settings status = %d", method.Code)
	}
	if allow := method.Header().Get("Allow"); allow != "GET, PUT" {
		t.Fatalf("settings Allow = %q, want GET, PUT", allow)
	}
	notFound := fixture.request(t, http.MethodGet, "/api/v1/settings/nope", "", session, "", "")
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("unknown settings path status = %d", notFound.Code)
	}
}

func TestSettingsTelegramConfigureTestAndClear(t *testing.T) {
	fixture := newAuthFixture(t, auth.DefaultRateLimitConfig(), true)
	session, csrf, _ := fixture.login(t, authTestPass)

	noCSRF := fixture.request(t, http.MethodPut, "/api/v1/settings/telegram",
		telegramConfigBody(settingsTestToken, settingsTestChatID, false), session, "", authOrigin)
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("telegram config without CSRF status = %d", noCSRF.Code)
	}
	configured := fixture.request(t, http.MethodPut, "/api/v1/settings/telegram",
		telegramConfigBody(settingsTestToken, settingsTestChatID, false), session, csrf, authOrigin)
	if configured.Code != http.StatusOK {
		t.Fatalf("configure telegram status = %d, body=%s", configured.Code, configured.Body.String())
	}
	body := configured.Body.String()
	if strings.Contains(body, settingsTestToken) || strings.Contains(body, settingsTestChatID) {
		t.Fatalf("configure response leaked credentials: %s", body)
	}
	payload := decodeSettings(t, configured.Body.Bytes())
	if !payload.Telegram.Available || !payload.Telegram.Configured || !payload.Telegram.Readable {
		t.Fatalf("configured telegram = %+v", payload.Telegram)
	}
	if payload.Telegram.ChatLabel != "chat ****7890" {
		t.Fatalf("chat label = %q", payload.Telegram.ChatLabel)
	}
	if payload.Telegram.LastTestAt != nil || payload.Telegram.LastTestOK != nil {
		t.Fatalf("configured test metadata = %+v", payload.Telegram)
	}

	var storedValue string
	var isSecret int
	if err := fixture.db.QueryRow(`SELECT value, is_secret FROM settings WHERE key = ?`,
		telegramsettings.TelegramBotTokenSetting).Scan(&storedValue, &isSecret); err != nil {
		t.Fatalf("read stored token: %v", err)
	}
	if isSecret != 1 || storedValue == settingsTestToken || strings.Contains(storedValue, settingsTestToken) {
		t.Fatalf("stored token is not encrypted: secret=%d value=%q", isSecret, storedValue)
	}

	noTestCSRF := fixture.request(t, http.MethodPost, "/api/v1/settings/telegram/test", "", session, "", authOrigin)
	if noTestCSRF.Code != http.StatusForbidden {
		t.Fatalf("telegram test without CSRF status = %d", noTestCSRF.Code)
	}
	tested := fixture.request(t, http.MethodPost, "/api/v1/settings/telegram/test", "", session, csrf, authOrigin)
	if tested.Code != http.StatusOK {
		t.Fatalf("telegram test status = %d, body=%s", tested.Code, tested.Body.String())
	}
	if strings.Contains(tested.Body.String(), settingsTestToken) || strings.Contains(tested.Body.String(), settingsTestChatID) {
		t.Fatalf("test response leaked credentials: %s", tested.Body.String())
	}
	testedPayload := decodeSettings(t, tested.Body.Bytes())
	if testedPayload.Telegram.LastTestOK == nil || !*testedPayload.Telegram.LastTestOK ||
		testedPayload.Telegram.LastTestAt == nil || !testedPayload.Telegram.LastTestAt.Equal(fixture.clock.now) {
		t.Fatalf("successful test metadata = %+v", testedPayload.Telegram)
	}
	if fixture.telegramTester.getMeCalls != 1 || fixture.telegramTester.sendCalls != 1 {
		t.Fatalf("telegram tester calls = %d/%d", fixture.telegramTester.getMeCalls, fixture.telegramTester.sendCalls)
	}
	if !strings.Contains(fixture.telegramTester.lastText, "Neda test notification") {
		t.Fatalf("test message = %q", fixture.telegramTester.lastText)
	}

	fixture.telegramTester.setErrors(&notify.TelegramError{
		Kind: notify.TelegramErrorPermanent, Op: "getMe", StatusCode: 403,
	}, nil)
	failed := fixture.request(t, http.MethodPost, "/api/v1/settings/telegram/test", "", session, csrf, authOrigin)
	if failed.Code != http.StatusOK {
		t.Fatalf("failed telegram test status = %d, body=%s", failed.Code, failed.Body.String())
	}
	failedPayload := decodeSettings(t, failed.Body.Bytes())
	if failedPayload.Telegram.LastTestOK == nil || *failedPayload.Telegram.LastTestOK ||
		failedPayload.Telegram.LastErrorKind != string(notify.TelegramErrorPermanent) {
		t.Fatalf("failed test metadata = %+v", failedPayload.Telegram)
	}
	if fixture.telegramTester.sendCalls != 1 {
		t.Fatalf("send calls after getMe failure = %d, want 1", fixture.telegramTester.sendCalls)
	}

	clearWithSecret := fixture.request(t, http.MethodPut, "/api/v1/settings/telegram",
		telegramConfigBody(settingsTestToken, "", true), session, csrf, authOrigin)
	if clearWithSecret.Code != http.StatusBadRequest {
		t.Fatalf("clear with token status = %d", clearWithSecret.Code)
	}
	cleared := fixture.request(t, http.MethodPut, "/api/v1/settings/telegram",
		telegramConfigBody("", "", true), session, csrf, authOrigin)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear telegram status = %d, body=%s", cleared.Code, cleared.Body.String())
	}
	clearedPayload := decodeSettings(t, cleared.Body.Bytes())
	if clearedPayload.Telegram.Configured || clearedPayload.Telegram.Readable || clearedPayload.Telegram.ChatLabel != "" {
		t.Fatalf("cleared telegram = %+v", clearedPayload.Telegram)
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key IN (?, ?)`,
		telegramsettings.TelegramBotTokenSetting, telegramsettings.TelegramChatIDSetting).Scan(&count); err != nil {
		t.Fatalf("count telegram settings: %v", err)
	}
	if count != 0 {
		t.Fatalf("telegram settings after clear = %d, want 0", count)
	}
	testAfterClear := fixture.request(t, http.MethodPost, "/api/v1/settings/telegram/test", "", session, csrf, authOrigin)
	if testAfterClear.Code != http.StatusConflict {
		t.Fatalf("test after clear status = %d, body=%s", testAfterClear.Code, testAfterClear.Body.String())
	}
	if !strings.Contains(testAfterClear.Body.String(), "telegram_not_configured") {
		t.Fatalf("test after clear error = %s", testAfterClear.Body.String())
	}

	badToken := fixture.request(t, http.MethodPut, "/api/v1/settings/telegram",
		telegramConfigBody("bad token", settingsTestChatID, false), session, csrf, authOrigin)
	if badToken.Code != http.StatusBadRequest {
		t.Fatalf("bad token status = %d, body=%s", badToken.Code, badToken.Body.String())
	}
	var tokenError errorPayload
	if err := json.Unmarshal(badToken.Body.Bytes(), &tokenError); err != nil {
		t.Fatalf("decode bad token error: %v", err)
	}
	if tokenError.Error.Fields["token"] == "" {
		t.Fatalf("bad token fields = %+v", tokenError.Error)
	}
	badChat := fixture.request(t, http.MethodPut, "/api/v1/settings/telegram",
		telegramConfigBody(settingsTestToken, "not-a-chat", false), session, csrf, authOrigin)
	if badChat.Code != http.StatusBadRequest {
		t.Fatalf("bad chat status = %d, body=%s", badChat.Code, badChat.Body.String())
	}
	var chatError errorPayload
	if err := json.Unmarshal(badChat.Body.Bytes(), &chatError); err != nil {
		t.Fatalf("decode bad chat error: %v", err)
	}
	if chatError.Error.Fields["chatId"] == "" {
		t.Fatalf("bad chat fields = %+v", chatError.Error)
	}

	logs := fixture.logs.String()
	if strings.Contains(logs, settingsTestToken) || strings.Contains(logs, settingsTestChatID) {
		t.Fatalf("logs leaked telegram credentials: %s", logs)
	}
}

func TestSettingsTelegramUnavailableWithoutKey(t *testing.T) {
	fixture := newAuthFixtureWithTelegram(t, auth.DefaultRateLimitConfig(), true, false)
	session, csrf, _ := fixture.login(t, authTestPass)

	recorder := fixture.request(t, http.MethodGet, "/api/v1/settings", "", session, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("settings without key status = %d", recorder.Code)
	}
	payload := decodeSettings(t, recorder.Body.Bytes())
	if payload.Telegram.Available || payload.Telegram.Configured || payload.Telegram.Readable {
		t.Fatalf("telegram without key = %+v", payload.Telegram)
	}
	configure := fixture.request(t, http.MethodPut, "/api/v1/settings/telegram",
		telegramConfigBody(settingsTestToken, settingsTestChatID, false), session, csrf, authOrigin)
	if configure.Code != http.StatusServiceUnavailable {
		t.Fatalf("configure without key status = %d, body=%s", configure.Code, configure.Body.String())
	}
	if !strings.Contains(configure.Body.String(), "telegram_settings_unavailable") {
		t.Fatalf("configure without key error = %s", configure.Body.String())
	}
	test := fixture.request(t, http.MethodPost, "/api/v1/settings/telegram/test", "", session, csrf, authOrigin)
	if test.Code != http.StatusServiceUnavailable {
		t.Fatalf("test without key status = %d, body=%s", test.Code, test.Body.String())
	}
	poll := 90
	update := fixture.request(t, http.MethodPut, "/api/v1/settings", settingsUpdateBody(&poll, nil), session, csrf, authOrigin)
	if update.Code != http.StatusOK {
		t.Fatalf("poll update without key status = %d, body=%s", update.Code, update.Body.String())
	}
	if got := decodeSettings(t, update.Body.Bytes()); got.PollIntervalSeconds != 90 {
		t.Fatalf("poll update without key = %+v", got)
	}
}

func intPtr(value int) *int    { return &value }
func boolPtr(value bool) *bool { return &value }
