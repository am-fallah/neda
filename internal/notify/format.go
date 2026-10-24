package notify

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
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
// mrbilit.com URLs. This is the delivery-safe formatter; the full NTF-002
// presentation work (notably Jalali display) remains a separate task.
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
	return value.In(tehran).Format("2006-01-02 15:04") + " (Tehran)"
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
