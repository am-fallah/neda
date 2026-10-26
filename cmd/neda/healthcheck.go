package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/config"
)

const healthcheckTimeout = 5 * time.Second

func runHealthcheck(stdout, stderr io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "neda: configuration error: %v\n", err)
		return 1
	}
	target, hostHeader, err := healthTarget(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "neda: healthcheck target: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		fmt.Fprintf(stderr, "neda: readiness check request: %v\n", err)
		return 1
	}
	if hostHeader != "" {
		req.Host = hostHeader
	}
	client := &http.Client{
		Timeout: healthcheckTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "neda: readiness check failed: %v\n", err)
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "neda: readiness check GET %s returned HTTP %d\n", target, response.StatusCode)
		return 1
	}
	fmt.Fprintln(stdout, "ok")
	return 0
}

func healthTarget(cfg config.Config) (string, string, error) {
	host, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil {
		return "", "", fmt.Errorf("invalid NEDA_HTTP_ADDR: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	host = strings.Trim(host, "[]")
	target := "http://" + net.JoinHostPort(host, port) + "/readyz"
	hostHeader := ""
	if cfg.PublicURL != nil {
		hostHeader = cfg.PublicURL.Host
	}
	return target, hostHeader, nil
}
