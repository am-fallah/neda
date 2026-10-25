package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/am-fallah/neda/internal/provider"
)

func TestSearchErrorMappingAndLog(t *testing.T) {
	server, logs := testServer(t, Config{}, &fakeReadiness{})
	cases := []struct {
		name         string
		err          error
		wantStatus   int
		wantCode     string
		wantKind     string
		wantOp       string
		wantUpstream float64
	}{
		{
			name:         "pre-header timeout",
			err:          &provider.Error{Kind: provider.ErrTimeout, Op: "mrbilit.search.page"},
			wantStatus:   http.StatusGatewayTimeout,
			wantCode:     "provider_timeout",
			wantKind:     "timeout",
			wantOp:       "mrbilit.search.page",
			wantUpstream: 0,
		},
		{
			name:         "upstream 504",
			err:          &provider.Error{Kind: provider.ErrHTTPStatus, Op: "mrbilit.search.page", StatusCode: http.StatusGatewayTimeout},
			wantStatus:   http.StatusBadGateway,
			wantCode:     "provider_error",
			wantKind:     "http_status",
			wantOp:       "mrbilit.search.page",
			wantUpstream: http.StatusGatewayTimeout,
		},
		{
			name:         "body-read timeout",
			err:          &provider.Error{Kind: provider.ErrTimeout, Op: "mrbilit.search.page.read", StatusCode: http.StatusOK},
			wantStatus:   http.StatusGatewayTimeout,
			wantCode:     "provider_timeout",
			wantKind:     "timeout",
			wantOp:       "mrbilit.search.page.read",
			wantUpstream: http.StatusOK,
		},
		{
			name:         "body-read network error",
			err:          &provider.Error{Kind: provider.ErrNetwork, Op: "mrbilit.search.page.read", StatusCode: http.StatusOK},
			wantStatus:   http.StatusBadGateway,
			wantCode:     "provider_error",
			wantKind:     "network",
			wantOp:       "mrbilit.search.page.read",
			wantUpstream: http.StatusOK,
		},
		{
			name:         "caller deadline",
			err:          &provider.Error{Kind: provider.ErrTimeout, Op: "mrbilit.http"},
			wantStatus:   http.StatusGatewayTimeout,
			wantCode:     "provider_timeout",
			wantKind:     "timeout",
			wantOp:       "mrbilit.http",
			wantUpstream: 0,
		},
		{
			name:         "caller cancellation",
			err:          &provider.Error{Kind: provider.ErrCanceled, Op: "mrbilit.http"},
			wantStatus:   http.StatusRequestTimeout,
			wantCode:     "provider_canceled",
			wantKind:     "canceled",
			wantOp:       "mrbilit.http",
			wantUpstream: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/providers/mrbilit/search", nil)
			server.writeSearchError(recorder, request, tc.err)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			var payload struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if payload.Error.Code != tc.wantCode {
				t.Fatalf("error code = %q, want %q", payload.Error.Code, tc.wantCode)
			}

			record := findProviderSearchLog(t, logs.String())
			if record["kind"] != tc.wantKind {
				t.Fatalf("log kind = %v, want %q", record["kind"], tc.wantKind)
			}
			if record["op"] != tc.wantOp {
				t.Fatalf("log op = %v, want %s", record["op"], tc.wantOp)
			}
			upstream, ok := record["upstream_status"].(float64)
			if !ok || upstream != tc.wantUpstream {
				t.Fatalf("log upstream_status = %v, want %v", record["upstream_status"], tc.wantUpstream)
			}
		})
	}
}

func findProviderSearchLog(t *testing.T, output string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line: %v", err)
		}
		if record["msg"] == "provider search failed" {
			return record
		}
	}
	t.Fatalf("provider search failure log not found in %s", output)
	return nil
}
