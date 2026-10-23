package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ErrOriginNotAllowed reports a missing or cross-origin Origin/Referer.
var ErrOriginNotAllowed = errors.New("auth: origin not allowed")

// OriginPolicy validates same-origin requests for login and state-changing
// authenticated routes. Production uses NEDA_PUBLIC_URL; development derives
// the expected origin from the validated request Host.
type OriginPolicy struct {
	PublicURL  *url.URL
	Production bool
}

type origin struct {
	scheme string
	host   string
	port   string
}

// Check requires a same-origin Origin header, falling back to Referer when
// Origin is absent. Both missing is a rejection.
func (p OriginPolicy) Check(r *http.Request) error {
	if r == nil {
		return ErrOriginNotAllowed
	}
	expected, err := p.expectedOrigin(r)
	if err != nil {
		return ErrOriginNotAllowed
	}

	if raw := strings.TrimSpace(r.Header.Get("Origin")); raw != "" {
		got, err := parseOrigin(raw)
		if err != nil || !expected.equal(got) {
			return ErrOriginNotAllowed
		}
		return nil
	}

	raw := strings.TrimSpace(r.Header.Get("Referer"))
	if raw == "" {
		return ErrOriginNotAllowed
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ErrOriginNotAllowed
	}
	got, err := originFromURL(parsed)
	if err != nil || !expected.equal(got) {
		return ErrOriginNotAllowed
	}
	return nil
}

func (p OriginPolicy) expectedOrigin(r *http.Request) (origin, error) {
	if p.PublicURL != nil {
		return originFromURL(p.PublicURL)
	}
	if p.Production {
		return origin{}, errors.New("auth: production origin policy requires public URL")
	}
	if strings.TrimSpace(r.Host) == "" {
		return origin{}, errors.New("auth: request host is empty")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	parsed, err := url.Parse(scheme + "://" + r.Host)
	if err != nil {
		return origin{}, err
	}
	return originFromURL(parsed)
}

func parseOrigin(raw string) (origin, error) {
	if strings.EqualFold(raw, "null") {
		return origin{}, errors.New("auth: null origin")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return origin{}, err
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return origin{}, errors.New("auth: origin contains unsupported components")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return origin{}, errors.New("auth: origin contains a path")
	}
	return originFromURL(parsed)
}

func originFromURL(parsed *url.URL) (origin, error) {
	if parsed == nil {
		return origin{}, errors.New("auth: nil origin URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return origin{}, errors.New("auth: origin scheme must be http or https")
	}
	if parsed.User != nil {
		return origin{}, errors.New("auth: origin userinfo is not allowed")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return origin{}, errors.New("auth: origin host is required")
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return origin{}, errors.New("auth: origin port is invalid")
	}
	return origin{scheme: scheme, host: host, port: port}, nil
}

func (o origin) equal(other origin) bool {
	return o.scheme == other.scheme && o.host == other.host && o.port == other.port
}
