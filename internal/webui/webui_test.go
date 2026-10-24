package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<!doctype html><html><body>Neda</body></html>")},
		"assets/app.js":  &fstest.MapFile{Data: []byte("console.log('neda')")},
		"assets/app.css": &fstest.MapFile{Data: []byte("body{}")},
		"favicon.svg":    &fstest.MapFile{Data: []byte("<svg></svg>")},
		"theme-init.js":  &fstest.MapFile{Data: []byte("void 0")},
		"sw.js":          &fstest.MapFile{Data: []byte("self")},
	}
}

func serve(t *testing.T, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestHandlerServesIndexAndSPAFallback(t *testing.T) {
	handler := New(testFS())
	if !handler.Built() {
		t.Fatal("Built() = false, want true")
	}

	for _, target := range []string{"/", "/monitors", "/monitors/abc", "/events/xyz"} {
		response := serve(t, handler, http.MethodGet, target)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, response.Code)
		}
		if !strings.Contains(response.Body.String(), "Neda") {
			t.Fatalf("%s: body = %q", target, response.Body.String())
		}
		if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Fatalf("%s: content type = %q", target, got)
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s: cache control = %q", target, got)
		}
		if got := response.Header().Get("Content-Security-Policy"); got != SPAHeaderPolicy {
			t.Fatalf("%s: CSP = %q", target, got)
		}
	}
}

func TestHandlerServesHashedAssetsWithImmutableCache(t *testing.T) {
	handler := New(testFS())
	response := serve(t, handler, http.MethodGet, "/assets/app.js")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if response.Body.String() != "console.log('neda')" {
		t.Fatalf("body = %q", response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("cache control = %q", got)
	}
	if got := response.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("content type = %q", got)
	}
}

func TestHandlerUsesNoCacheForServiceWorkerAndThemeBootstrap(t *testing.T) {
	handler := New(testFS())
	for _, target := range []string{"/sw.js", "/theme-init.js"} {
		response := serve(t, handler, http.MethodGet, target)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", target, response.Code)
		}
		if got := response.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s: cache control = %q", target, got)
		}
	}
}

func TestHandlerDoesNotFallbackForMissingAssetsOrFiles(t *testing.T) {
	handler := New(testFS())
	for _, target := range []string{"/assets/missing.js", "/missing.js", "/manifest.webmanifest"} {
		response := serve(t, handler, http.MethodGet, target)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, response.Code)
		}
		if strings.Contains(response.Body.String(), "Neda") {
			t.Fatalf("%s: leaked SPA shell for missing asset", target)
		}
	}
}

func TestHandlerSupportsHeadAndRejectsOtherMethods(t *testing.T) {
	handler := New(testFS())
	head := serve(t, handler, http.MethodHead, "/")
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", head.Code)
	}
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD body length = %d, want 0", head.Body.Len())
	}
	if head.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD content length is missing")
	}

	post := serve(t, handler, http.MethodPost, "/")
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", post.Code)
	}
	if post.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("Allow = %q", post.Header().Get("Allow"))
	}
}

func TestHandlerReportsMissingBuildWithoutServingFiles(t *testing.T) {
	handler := New(fstest.MapFS{
		"assets/app.js": &fstest.MapFile{Data: []byte("x")},
	})
	if handler.Built() {
		t.Fatal("Built() = true without index.html")
	}
	response := serve(t, handler, http.MethodGet, "/")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	if !strings.Contains(response.Body.String(), "not built") {
		t.Fatalf("body = %q", response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing-build response must not be cached")
	}
}

func TestCleanAssetPathRejectsUnsafePaths(t *testing.T) {
	for _, input := range []string{"../secret", "/../secret", "/a/../../secret"} {
		name, ok := cleanAssetPath(input)
		if ok && strings.Contains(name, "..") {
			t.Fatalf("unsafe path %q accepted as %q", input, name)
		}
	}
	if _, ok := cleanAssetPath(""); !ok {
		t.Fatal("empty path should be the SPA root")
	}
	if name, ok := cleanAssetPath("/assets/app.js"); !ok || name != "assets/app.js" {
		t.Fatalf("cleanAssetPath = %q, %v", name, ok)
	}
}

func TestNewWithNilFSIsNotBuilt(t *testing.T) {
	handler := New(nil)
	if handler.Built() {
		t.Fatal("Built() = true for nil FS")
	}
}
