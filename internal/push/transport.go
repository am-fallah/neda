package push

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const (
	defaultSendTimeout  = 10 * time.Second
	defaultDialTimeout  = 5 * time.Second
	maxPushResponseBody = 32 * 1024
)

var (
	errPushRedirect   = errors.New("push: redirect rejected")
	errBlockedAddress = errors.New("push: blocked destination address")
)

// Resolver is the DNS surface used by the guarded transport.
type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type dialFunc func(context.Context, string, string) (net.Conn, error)

// guardedDialer resolves a host once, rejects every non-public address, dials
// a validated IP literal, and rechecks the address actually connected. This
// prevents DNS rebinding between validation and connection.
type guardedDialer struct {
	resolver   Resolver
	timeout    time.Duration
	dial       dialFunc
	allowTests bool
}

func newGuardedDialer(resolver Resolver, timeout time.Duration) *guardedDialer {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	return &guardedDialer{
		resolver: resolver,
		timeout:  timeout,
		dial: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
}

func (d *guardedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d == nil {
		return nil, errors.New("push: nil dialer")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("push: invalid dial address: %w", err)
	}
	if host == "" || port == "" {
		return nil, errors.New("push: dial address is incomplete")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !d.allowed(ip) {
			return nil, errBlockedAddress
		}
	} else {
		addresses, err := d.resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("push: resolve push host: %w", err)
		}
		if len(addresses) == 0 {
			return nil, errors.New("push: push host resolved to no addresses")
		}
		// Resolve all records and fail closed if any answer is unsafe. The
		// selected address is then dialed directly, so a second DNS answer
		// cannot redirect the connection.
		selected := ""
		for _, address := range addresses {
			if !d.allowed(address.IP) {
				return nil, errBlockedAddress
			}
			if selected == "" {
				selected = address.IP.String()
			}
		}
		host = selected
	}

	dialCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	conn, err := d.dial(dialCtx, network, net.JoinHostPort(host, port))
	if err != nil {
		return nil, fmt.Errorf("push: dial: %w", err)
	}
	if err := verifyConnectedAddress(conn, d.allowed); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (d *guardedDialer) allowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if d.allowTests && (ip.IsLoopback() || ip.IsUnspecified()) {
		return true
	}
	return !IsBlockedIP(ip)
}

func verifyConnectedAddress(conn net.Conn, allowed func(net.IP) bool) error {
	if conn == nil || allowed == nil {
		return errors.New("push: missing connection or validator")
	}
	remote := conn.RemoteAddr()
	if remote == nil {
		return errors.New("push: connection has no remote address")
	}
	tcp, ok := remote.(*net.TCPAddr)
	if !ok {
		// Test doubles may not expose a TCP address. Real transports always do.
		return nil
	}
	if !allowed(tcp.IP) {
		return errBlockedAddress
	}
	return nil
}

func newHTTPClient(resolver Resolver, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultSendTimeout
	}
	dialer := newGuardedDialer(resolver, defaultDialTimeout)
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   defaultDialTimeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		DisableKeepAlives:     true,
		MaxIdleConns:          0,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errPushRedirect
		},
	}
}

// permitTestAddresses is used only by package tests that need a local HTTP
// fixture. It is not exported and production callers cannot reach it.
func permitTestAddresses(client *http.Client) {
	if client == nil {
		return
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		return
	}
	dialer := newGuardedDialer(net.DefaultResolver, defaultDialTimeout)
	dialer.allowTests = true
	transport.DialContext = dialer.DialContext
}
