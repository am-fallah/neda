package notify

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/jalali"
)

var tehran = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// FormatTelegram renders one durable event as Telegram HTML. All dynamic text
// is escaped. Booking links are emitted only when they are validated HTTPS
// mrbilit.com URLs. Instants are displayed in Tehran time using the Persian
// calendar; Gregorian values remain the persisted canonical form.
func FormatTelegram(event Event) (string, error) {
	switch event.Kind {
	case KindAvailability:
		return formatAvailabilityTelegram(event), nil
	case KindProviderDegraded:
		return formatProviderHealthTelegram(event, false), nil
	case KindProviderRecovered:
		return formatProviderHealthTelegram(event, true), nil
	default:
		return "", fmt.Errorf("notify: unsupported event kind %q", event.Kind)
	}
}

func formatAvailabilityTelegram(event Event) string {
	route := joinRoute(event.OriginName, event.DestinationName)
	lines := []string{"<b>Ticket available</b>"}
	if route != "" {
		lines = append(lines, EscapeTelegramHTML(route))
	}
	if event.DepartureAt != nil {
		lines = append(lines, "Departure: "+EscapeTelegramHTML(formatEventTime(*event.DepartureAt)))
	}
	if event.ArrivalAt != nil {
		lines = append(lines, "Arrival: "+EscapeTelegramHTML(formatEventTime(*event.ArrivalAt)))
	}
	train := strings.TrimSpace(event.TrainNumber)
	if train != "" {
		if operator := strings.TrimSpace(event.Operator); operator != "" {
			train += " (" + operator + ")"
		}
		lines = append(lines, "Train: "+EscapeTelegramHTML(train))
	}
	vehicle := strings.TrimSpace(event.WagonName)
	class := strings.TrimSpace(event.ClassName)
	switch {
	case vehicle != "" && class != "":
		lines = append(lines, "Wagon/class: "+EscapeTelegramHTML(vehicle+" / "+class))
	case vehicle != "":
		lines = append(lines, "Wagon: "+EscapeTelegramHTML(vehicle))
	case class != "":
		lines = append(lines, "Class: "+EscapeTelegramHTML(class))
	}
	if event.PriceAmount != nil && strings.TrimSpace(event.PriceCurrency) != "" {
		lines = append(lines, "Price: "+EscapeTelegramHTML(strconv.FormatInt(*event.PriceAmount, 10)+" "+event.PriceCurrency))
	}
	if provider := displayProvider(event.ProviderID); provider != "" {
		lines = append(lines, "Provider: "+EscapeTelegramHTML(provider))
	}
	if !event.OccurredAt.IsZero() {
		lines = append(lines, "Detected: "+EscapeTelegramHTML(formatEventTime(event.OccurredAt)))
	}
	if booking := validatedBookingURL(event.BookingURL); booking != "" {
		lines = append(lines, `<a href="`+EscapeTelegramHTML(booking)+`">Open booking</a>`)
	}
	return strings.Join(lines, "\n")
}

func formatProviderHealthTelegram(event Event, recovered bool) string {
	provider := displayProvider(event.ProviderID)
	if provider == "" {
		provider = "provider"
	}
	var lines []string
	if recovered {
		lines = append(lines, "<b>Provider recovered: "+EscapeTelegramHTML(provider)+"</b>")
		lines = append(lines, "Ticket availability checks are succeeding again.")
	} else {
		lines = append(lines, "<b>Provider issue: "+EscapeTelegramHTML(provider)+"</b>")
		if event.FailureCount != nil {
			lines = append(lines, "Consecutive failures: "+EscapeTelegramHTML(strconv.Itoa(*event.FailureCount)))
		}
		if kind := strings.TrimSpace(event.ErrorKind); kind != "" {
			lines = append(lines, "Category: "+EscapeTelegramHTML(kind))
		}
		lines = append(lines, "Ticket availability is unknown; this is not a sold-out verdict.")
	}
	if !event.OccurredAt.IsZero() {
		lines = append(lines, "Detected: "+EscapeTelegramHTML(formatEventTime(event.OccurredAt)))
	}
	return strings.Join(lines, "\n")
}

func joinRoute(origin, destination string) string {
	origin = strings.TrimSpace(origin)
	destination = strings.TrimSpace(destination)
	switch {
	case origin == "":
		return destination
	case destination == "":
		return origin
	default:
		return origin + " → " + destination
	}
}

func formatEventTime(value time.Time) string {
	formatted := jalali.FormatDateTime(value.In(tehran))
	if formatted == "" {
		return ""
	}
	return formatted + " (Tehran)"
}

// PushPayload is the small JSON document consumed by the service worker. It
// contains no credentials or subscription material.
type PushPayload struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	URL     string `json:"url,omitempty"`
	Tag     string `json:"tag,omitempty"`
	EventID string `json:"eventId,omitempty"`
	Kind    string `json:"kind"`
}

const maxPushPayloadBytes = 3500

// FormatPush renders one durable event as a bounded Web Push payload. The URL
// is a validated booking URL when present, otherwise the authenticated event
// detail page on publicURL. Unknown or unvalidated URL shapes are omitted.
func FormatPush(event Event, publicURL string) ([]byte, error) {
	var payload PushPayload
	switch event.Kind {
	case KindAvailability:
		payload = formatAvailabilityPush(event, publicURL)
	case KindProviderDegraded:
		payload = formatProviderHealthPush(event, publicURL, false)
	case KindProviderRecovered:
		payload = formatProviderHealthPush(event, publicURL, true)
	default:
		return nil, fmt.Errorf("notify: unsupported event kind %q", event.Kind)
	}
	payload.Kind = string(event.Kind)
	payload.EventID = event.ID
	if event.ID != "" {
		payload.Tag = "neda-event-" + event.ID
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("notify: encode push payload: %w", err)
	}
	if len(encoded) > maxPushPayloadBytes {
		return nil, fmt.Errorf("notify: push payload exceeds %d bytes", maxPushPayloadBytes)
	}
	return encoded, nil
}

func formatAvailabilityPush(event Event, publicURL string) PushPayload {
	payload := PushPayload{Title: "Ticket available"}
	lines := make([]string, 0, 7)
	if route := joinRoute(event.OriginName, event.DestinationName); route != "" {
		lines = append(lines, route)
	}
	if event.DepartureAt != nil {
		if value := formatEventTime(*event.DepartureAt); value != "" {
			lines = append(lines, "Departure: "+value)
		}
	}
	if train := strings.TrimSpace(event.TrainNumber); train != "" {
		if operator := strings.TrimSpace(event.Operator); operator != "" {
			train += " (" + operator + ")"
		}
		lines = append(lines, "Train: "+train)
	}
	switch value := strings.TrimSpace(event.WagonName); {
	case value != "" && strings.TrimSpace(event.ClassName) != "":
		lines = append(lines, "Wagon/class: "+value+" / "+strings.TrimSpace(event.ClassName))
	case value != "":
		lines = append(lines, "Wagon: "+value)
	case strings.TrimSpace(event.ClassName) != "":
		lines = append(lines, "Class: "+strings.TrimSpace(event.ClassName))
	}
	if event.PriceAmount != nil && strings.TrimSpace(event.PriceCurrency) != "" {
		lines = append(lines, "Price: "+strconv.FormatInt(*event.PriceAmount, 10)+" "+event.PriceCurrency)
	}
	if provider := displayProvider(event.ProviderID); provider != "" {
		lines = append(lines, "Provider: "+provider)
	}
	if value := formatEventTime(event.OccurredAt); value != "" {
		lines = append(lines, "Detected: "+value)
	}
	payload.Body = strings.Join(lines, "\n")
	payload.URL = pushTargetURL(event, publicURL)
	return payload
}

func formatProviderHealthPush(event Event, publicURL string, recovered bool) PushPayload {
	provider := displayProvider(event.ProviderID)
	if provider == "" {
		provider = "provider"
	}
	payload := PushPayload{Title: "Provider recovered: " + provider}
	if !recovered {
		payload.Title = "Provider issue: " + provider
	}
	lines := make([]string, 0, 4)
	if recovered {
		lines = append(lines, "Ticket availability checks are succeeding again.")
	} else {
		if event.FailureCount != nil {
			lines = append(lines, "Consecutive failures: "+strconv.Itoa(*event.FailureCount))
		}
		if kind := strings.TrimSpace(event.ErrorKind); kind != "" {
			lines = append(lines, "Category: "+kind)
		}
		lines = append(lines, "Ticket availability is unknown; this is not a sold-out verdict.")
	}
	if value := formatEventTime(event.OccurredAt); value != "" {
		lines = append(lines, "Detected: "+value)
	}
	payload.Body = strings.Join(lines, "\n")
	payload.URL = pushTargetURL(event, publicURL)
	return payload
}

func pushTargetURL(event Event, publicURL string) string {
	if booking := validatedBookingURL(event.BookingURL); booking != "" {
		return booking
	}
	base := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if base == "" || event.ID == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return ""
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return base + "/events/" + url.PathEscape(event.ID)
}

func displayProvider(providerID string) string {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "mrbilit":
		return "MrBilit"
	case "":
		return ""
	default:
		return strings.TrimSpace(providerID)
	}
}

// validatedBookingURL accepts only the illustrative HTTPS mrbilit.com booking
// host. The adapter still owns slug/date construction (PRV-007); this function
// only prevents an unvalidated stored URL from becoming an active link.
func validatedBookingURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "mrbilit.com") {
		return ""
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return ""
	}
	return parsed.String()
}
