// Package config loads and validates Neda's runtime configuration.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment names the runtime mode. Production enables fail-closed checks.
type Environment string

const (
	Development Environment = "development"
	Production  Environment = "production"
)

// Config is the validated runtime configuration. It intentionally does not
// retain secret material; key loading belongs to the component that needs it.
type Config struct {
	Env                 Environment
	HTTPAddr            string
	DBPath              string
	EncryptionKeyFile   string
	PublicURL           *url.URL
	VAPIDSubject        string
	LogLevel            string
	LogFormat           string
	TrustProxy          bool
	FakeProviderEnabled bool
	SessionTTL          time.Duration
	SessionIdle         time.Duration
	Workers             int
}

// IsProduction reports whether production-only safeguards are enabled.
func (c Config) IsProduction() bool {
	return c.Env == Production
}

// Load reads configuration from the process environment.
func Load() (Config, error) {
	return LoadFrom(os.LookupEnv)
}

// LoadFrom reads configuration from the supplied lookup function. It is
// exported for deterministic tests and for callers that provide their own
// environment source.
func LoadFrom(get func(string) (string, bool)) (Config, error) {
	if get == nil {
		return Config{}, errors.New("config: nil environment lookup")
	}

	env, err := parseEnvironment(value(get, "NEDA_ENV", string(Production)))
	if err != nil {
		return Config{}, err
	}

	httpAddr := value(get, "NEDA_HTTP_ADDR", ":8080")
	if err := validateListenAddr(httpAddr); err != nil {
		return Config{}, fmt.Errorf("NEDA_HTTP_ADDR: %w", err)
	}

	dbPath := value(get, "NEDA_DB_PATH", "/data/neda.db")
	if strings.TrimSpace(dbPath) == "" {
		return Config{}, errors.New("NEDA_DB_PATH must not be empty")
	}

	logLevel := strings.ToLower(value(get, "NEDA_LOG_LEVEL", "info"))
	switch logLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("NEDA_LOG_LEVEL: unsupported value %q", logLevel)
	}

	defaultLogFormat := "json"
	if env == Development {
		defaultLogFormat = "text"
	}
	logFormat := strings.ToLower(value(get, "NEDA_LOG_FORMAT", defaultLogFormat))
	switch logFormat {
	case "json", "text":
	default:
		return Config{}, fmt.Errorf("NEDA_LOG_FORMAT: unsupported value %q", logFormat)
	}

	trustProxy, err := parseBool(get, "NEDA_TRUST_PROXY", false)
	if err != nil {
		return Config{}, err
	}
	fakeProvider, err := parseBool(get, "NEDA_FAKE_PROVIDER_ENABLED", false)
	if err != nil {
		return Config{}, err
	}

	sessionTTL, err := parseHours(get, "NEDA_SESSION_TTL_HOURS", 720)
	if err != nil {
		return Config{}, err
	}
	sessionIdle, err := parseHours(get, "NEDA_SESSION_IDLE_HOURS", 168)
	if err != nil {
		return Config{}, err
	}
	if sessionIdle > sessionTTL {
		return Config{}, errors.New("NEDA_SESSION_IDLE_HOURS must not exceed NEDA_SESSION_TTL_HOURS")
	}

	workers, err := parseInt(get, "NEDA_WORKERS", 2, 1, 16)
	if err != nil {
		return Config{}, err
	}

	publicURL, err := parsePublicURL(value(get, "NEDA_PUBLIC_URL", ""), env)
	if err != nil {
		return Config{}, err
	}

	vapidSubject := value(get, "NEDA_VAPID_SUBJECT", "")
	if vapidSubject != "" {
		if err := validateVAPIDSubject(vapidSubject); err != nil {
			return Config{}, err
		}
	}

	keyFile := value(get, "NEDA_ENCRYPTION_KEY_FILE", "")
	if keyFile == "" {
		if env == Production {
			return Config{}, errors.New("NEDA_ENCRYPTION_KEY_FILE is required in production")
		}
	} else if err := validateKeyFile(keyFile); err != nil {
		return Config{}, err
	}

	return Config{
		Env:                 env,
		HTTPAddr:            httpAddr,
		DBPath:              dbPath,
		EncryptionKeyFile:   keyFile,
		PublicURL:           publicURL,
		VAPIDSubject:        vapidSubject,
		LogLevel:            logLevel,
		LogFormat:           logFormat,
		TrustProxy:          trustProxy,
		FakeProviderEnabled: fakeProvider,
		SessionTTL:          time.Duration(sessionTTL) * time.Hour,
		SessionIdle:         time.Duration(sessionIdle) * time.Hour,
		Workers:             workers,
	}, nil
}

func value(get func(string) (string, bool), key, fallback string) string {
	raw, ok := get(key)
	if !ok {
		return fallback
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func parseEnvironment(raw string) (Environment, error) {
	switch Environment(strings.ToLower(strings.TrimSpace(raw))) {
	case Development:
		return Development, nil
	case Production:
		return Production, nil
	default:
		return "", fmt.Errorf("NEDA_ENV: unsupported value %q (want development or production)", raw)
	}
}

func validateListenAddr(addr string) error {
	if addr == "" {
		return errors.New("must not be empty")
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("invalid port %q", port)
	}
	return nil
}

func parseBool(get func(string) (string, bool), key string, fallback bool) (bool, error) {
	raw := value(get, key, strconv.FormatBool(fallback))
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: invalid boolean %q", key, raw)
	}
	return parsed, nil
}

func parseInt(get func(string) (string, bool), key string, fallback, min, max int) (int, error) {
	raw := value(get, key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q", key, raw)
	}
	if parsed < min || parsed > max {
		return 0, fmt.Errorf("%s: must be between %d and %d", key, min, max)
	}
	return parsed, nil
}

func parseHours(get func(string) (string, bool), key string, fallback int) (int, error) {
	return parseInt(get, key, fallback, 1, 24*365*10)
}

func parsePublicURL(raw string, env Environment) (*url.URL, error) {
	if raw == "" {
		if env == Production {
			return nil, errors.New("NEDA_PUBLIC_URL is required in production")
		}
		return nil, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("NEDA_PUBLIC_URL: invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && !(env == Development && parsed.Scheme == "http") {
		return nil, errors.New("NEDA_PUBLIC_URL: scheme must be https in production")
	}
	if parsed.Host == "" {
		return nil, errors.New("NEDA_PUBLIC_URL: host is required")
	}
	if parsed.User != nil {
		return nil, errors.New("NEDA_PUBLIC_URL: userinfo is not allowed")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("NEDA_PUBLIC_URL: query and fragment are not allowed")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("NEDA_PUBLIC_URL: path must be empty or /")
	}
	parsed.Path = ""
	return parsed, nil
}

func validateVAPIDSubject(raw string) error {
	if strings.HasPrefix(strings.ToLower(raw), "mailto:") {
		if strings.TrimSpace(raw[len("mailto:"):]) == "" {
			return errors.New("NEDA_VAPID_SUBJECT: mailto address is empty")
		}
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("NEDA_VAPID_SUBJECT: must be a mailto: address or https URL")
	}
	return nil
}

func validateKeyFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("NEDA_ENCRYPTION_KEY_FILE: unavailable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("NEDA_ENCRYPTION_KEY_FILE: must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New("NEDA_ENCRYPTION_KEY_FILE: permissions must be owner-only (0400 or 0600)")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("NEDA_ENCRYPTION_KEY_FILE: unreadable: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(contents)))
	if err != nil {
		return errors.New("NEDA_ENCRYPTION_KEY_FILE: contents must be base64")
	}
	if len(decoded) != 32 {
		return fmt.Errorf("NEDA_ENCRYPTION_KEY_FILE: decoded key must be exactly 32 bytes, got %d", len(decoded))
	}
	return nil
}
