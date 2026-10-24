package mrbilit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/provider"
	"golang.org/x/text/unicode/norm"
)

const keySeparator = "\x1f"

// normalizeText applies the architecture's display-text normalization: NFC,
// trim outer whitespace, and collapse internal whitespace runs.
func normalizeText(value string) string {
	value = norm.NFC.String(value)
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.Join(strings.Fields(value), " ")
}

// normalizeNativeID preserves the native identifier's case and internal
// characters, trimming only accidental outer whitespace.
func normalizeNativeID(value string) string {
	return strings.TrimSpace(value)
}

func journeyKey(journey provider.Journey, departureDate, originID, destinationID string) (string, error) {
	if native := normalizeNativeID(journey.NativeID); native != "" {
		return "id:" + native, nil
	}
	if strings.TrimSpace(journey.TrainNumber) == "" || journey.DepartureAt == nil ||
		strings.TrimSpace(originID) == "" || strings.TrimSpace(destinationID) == "" ||
		strings.TrimSpace(journey.Operator) == "" {
		return "", errors.New("journey identity requires a native id or train number, departure time, route ids, and operator")
	}
	canonical := strings.Join([]string{
		"v1",
		departureDate,
		normalizeText(journey.TrainNumber),
		journey.DepartureAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(originID),
		strings.TrimSpace(destinationID),
		normalizeText(journey.Operator),
	}, keySeparator)
	return sha256Hex(canonical), nil
}

func offeringKey(offering provider.Offering, wagonID, compartmentType, shortTitle string) (string, bool) {
	if native := normalizeNativeID(offering.NativeID); native != "" {
		return "id:" + native, true
	}
	wagonID = normalizeText(wagonID)
	compartmentType = normalizeText(compartmentType)
	wagonName := normalizeText(offering.WagonName)
	shortTitle = normalizeText(shortTitle)
	className := normalizeText(offering.ClassName)
	if wagonID == "" && compartmentType == "" && wagonName == "" && shortTitle == "" && className == "" {
		return "", false
	}
	canonical := strings.Join([]string{
		"v1",
		wagonID,
		compartmentType,
		wagonName,
		shortTitle,
		className,
	}, keySeparator)
	return sha256Hex(canonical), true
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
