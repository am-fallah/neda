package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/am-fallah/neda/internal/webui"
)

func testWebUI() *webui.Handler {
	return webui.New(fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>Neda</title>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	})
}

func TestSPAFallbackAndAPI404Separation(t *testing.T) {
	server, _ := testServer(t, Config{WebUI: testWebUI()}, &fakeReadiness{})

	index := request(t, server, http.MethodGet, "http://localhost/", "localhost", nil)
	if index.Code != http.StatusOK {
		t.Fatalf("index status = %d; body=%s", index.Code, index.Body.String())
	}
	if got := index.Header().Get("Content-Security-Policy"); got != webui.SPAHeaderPolicy {
		t.Fatalf("index CSP = %q", got)
	}
	if got := index.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("index cache = %q", got)
	}

	deepLink := request(t, server, http.MethodGet, "http://localhost/monitors/abc", "localhost", nil)
	if deepLink.Code != http.StatusOK || !strings.Contains(deepLink.Body.String(), "Neda") {
		t.Fatalf("deep link status=%d body=%q", deepLink.Code, deepLink.Body.String())
	}

	asset := request(t, server, http.MethodGet, "http://localhost/assets/app.js", "localhost", nil)
	if asset.Code != http.StatusOK {
		t.Fatalf("asset status = %d", asset.Code)
	}
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache = %q", got)
	}

	for _, target := range []string{"/api", "/api/v1/unknown", "/api/v2/monitors"} {
		response := request(t, server, http.MethodGet, "http://localhost"+target, "localhost", nil)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, response.Code)
		}
		if !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s: content type = %q, want JSON", target, response.Header().Get("Content-Type"))
		}
		if got := response.Header().Get("Content-Security-Policy"); got != "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'" {
			t.Fatalf("%s: API CSP = %q", target, got)
		}
		if strings.Contains(response.Body.String(), "Neda") {
			t.Fatalf("%s: API 404 leaked the SPA shell", target)
		}
	}
}

func TestSPAFallbackLeavesHealthAndUnknownBackendPathsAlone(t *testing.T) {
	server, _ := testServer(t, Config{WebUI: testWebUI()}, &fakeReadiness{})
	health := request(t, server, http.MethodGet, "http://localhost/healthz", "localhost", nil)
	if health.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", health.Code)
	}
	for _, target := range []string{"/healthz/", "/readyz/"} {
		response := request(t, server, http.MethodGet, "http://localhost"+target, "localhost", nil)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, response.Code)
		}
	}
}

func TestSPAUnavailableWithoutBuild(t *testing.T) {
	server, _ := testServer(t, Config{WebUI: webui.New(fstest.MapFS{})}, &fakeReadiness{})
	response := request(t, server, http.MethodGet, "http://localhost/", "localhost", nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	api := request(t, server, http.MethodGet, "http://localhost/api/v1/unknown", "localhost", nil)
	if api.Code != http.StatusNotFound || !strings.Contains(api.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("API must stay JSON when frontend is unavailable: status=%d type=%q", api.Code, api.Header().Get("Content-Type"))
	}
}
