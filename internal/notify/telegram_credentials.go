package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

// TelegramCredentials holds a Telegram bot token and chat ID. Fields are
// deliberately unexported: callers can save, load, and present a label, and the
// TelegramClient can use the credentials, but there is no public getter that
// hands raw secret material to arbitrary code. All formatting and JSON
// rendering is redacted.
type TelegramCredentials struct {
	token  string
	chatID string
}

// NewTelegramCredentials validates and normalizes Telegram credentials.
func NewTelegramCredentials(token, chatID string) (TelegramCredentials, error) {
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	if err := validateTelegramToken(token); err != nil {
		return TelegramCredentials{}, err
	}
	if err := validateTelegramChatID(chatID); err != nil {
		return TelegramCredentials{}, err
	}
	return TelegramCredentials{token: token, chatID: chatID}, nil
}

// ChatLabel returns a non-secret display label. Numeric chat IDs are masked to
// their last four digits; public @usernames are shown as supplied.
func (c TelegramCredentials) ChatLabel() string {
	if c.chatID == "" {
		return ""
	}
	if strings.HasPrefix(c.chatID, "@") {
		return c.chatID
	}
	digits := strings.TrimPrefix(c.chatID, "-")
	if len(digits) <= 4 {
		return "chat ****"
	}
	return "chat ****" + digits[len(digits)-4:]
}

// Validate re-checks the credential shape without exposing either value.
func (c TelegramCredentials) Validate() error { return c.validate() }

func (c TelegramCredentials) validate() error {
	if err := validateTelegramToken(c.token); err != nil {
		return err
	}
	return validateTelegramChatID(c.chatID)
}

// String implements fmt.Stringer without exposing secrets.
func (c TelegramCredentials) String() string { return "notify.TelegramCredentials(redacted)" }

// GoString implements fmt.GoStringer without exposing secrets.
func (c TelegramCredentials) GoString() string { return "notify.TelegramCredentials(redacted)" }

// Format prevents all fmt verbs from printing credentials.
func (c TelegramCredentials) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, "notify.TelegramCredentials(redacted)")
}

// MarshalJSON never emits the token or raw chat ID.
func (c TelegramCredentials) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Configured bool   `json:"configured"`
		ChatLabel  string `json:"chat_label,omitempty"`
	}{Configured: c.token != "" && c.chatID != "", ChatLabel: c.ChatLabel()})
}

// ValidateTelegramToken validates a bot token without retaining it. Callers can
// use this to return field-specific settings errors before persistence.
func ValidateTelegramToken(token string) error {
	return validateTelegramToken(token)
}

// ValidateTelegramChatID validates a numeric or @username chat ID without
// retaining it.
func ValidateTelegramChatID(chatID string) error {
	return validateTelegramChatID(chatID)
}

func validateTelegramToken(token string) error {
	if token == "" {
		return fmt.Errorf("notify: telegram bot token is required")
	}
	if len(token) > 256 {
		return fmt.Errorf("notify: telegram bot token is too long")
	}
	if !strings.Contains(token, ":") {
		return fmt.Errorf("notify: telegram bot token must contain a colon")
	}
	for _, r := range token {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '/' {
			return fmt.Errorf("notify: telegram bot token contains an invalid character")
		}
	}
	return nil
}

func validateTelegramChatID(chatID string) error {
	if chatID == "" {
		return fmt.Errorf("notify: telegram chat id is required")
	}
	if len(chatID) > 64 {
		return fmt.Errorf("notify: telegram chat id is too long")
	}
	if strings.HasPrefix(chatID, "@") {
		username := strings.TrimPrefix(chatID, "@")
		if len(username) < 5 || len(username) > 64 {
			return fmt.Errorf("notify: telegram username chat id must be 5-64 characters")
		}
		for _, r := range username {
			if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return fmt.Errorf("notify: telegram username chat id contains an invalid character")
			}
		}
		return nil
	}
	value, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil || value == 0 {
		return fmt.Errorf("notify: telegram chat id must be a non-zero integer or @username")
	}
	return nil
}
