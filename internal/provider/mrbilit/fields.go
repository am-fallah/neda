package mrbilit

import (
	"encoding/json"
	"strconv"
	"strings"
)

// optionalString distinguishes absent, null, a valid string, and an
// unexpected JSON type without failing the whole envelope.
type optionalString struct {
	Present    bool
	Valid      bool
	Unexpected bool
	Value      string
}

func (o *optionalString) UnmarshalJSON(data []byte) error {
	o.Present = true
	if string(data) == "null" {
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		o.Unexpected = true
		return nil
	}
	o.Valid = true
	o.Value = value
	return nil
}

// optionalBool accepts only real JSON booleans. Strings and numbers are
// recorded as unexpected; they are never coerced into availability evidence.
type optionalBool struct {
	Present    bool
	Valid      bool
	Unexpected bool
	Value      bool
}

func (o *optionalBool) UnmarshalJSON(data []byte) error {
	o.Present = true
	if string(data) == "null" {
		return nil
	}
	switch string(data) {
	case "true":
		o.Valid = true
		o.Value = true
	case "false":
		o.Valid = true
		o.Value = false
	default:
		o.Unexpected = true
	}
	return nil
}

// optionalInt distinguishes absent/null, a canonical integer, a numeric value
// with an invalid form (fraction/exponent/overflow), and an unexpected type.
type optionalInt struct {
	Present    bool
	Valid      bool
	Unexpected bool
	Invalid    bool
	Value      int64
}

func (o *optionalInt) UnmarshalJSON(data []byte) error {
	o.Present = true
	if string(data) == "null" {
		return nil
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || (trimmed[0] != '-' && (trimmed[0] < '0' || trimmed[0] > '9')) {
		o.Unexpected = true
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		o.Unexpected = true
		return nil
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		o.Invalid = true
		return nil
	}
	o.Valid = true
	o.Value = value
	return nil
}

// optionalID accepts a JSON string or integer and preserves a canonical native
// identifier. Numeric identifiers are rendered as base-10 text; availability
// fields are never coerced this way.
type optionalID struct {
	Present    bool
	Valid      bool
	Unexpected bool
	Value      string
}

func (o *optionalID) UnmarshalJSON(data []byte) error {
	o.Present = true
	if string(data) == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		o.Valid = strings.TrimSpace(text) != ""
		o.Value = normalizeNativeID(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		o.Unexpected = true
		return nil
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		o.Unexpected = true
		return nil
	}
	o.Valid = true
	o.Value = strconv.FormatInt(value, 10)
	return nil
}
