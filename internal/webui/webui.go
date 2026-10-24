// Package webui serves the embedded single-page application and implements
// SPA fallback without intercepting API or health routes.
package webui

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// SPAHeaderPolicy is the Content-Security-Policy for the HTML shell. Scripts
// remain self-only. Inline styles are allowed because Radix/shadcn components
// set style attributes for positioning; no inline script is allowed. The theme
// bootstrap is an external same-origin file.
const SPAHeaderPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

type Handler struct {
	dist  fs.FS
	index []byte
	built bool
}

// New returns a handler for the supplied dist filesystem. A missing index.html
// is not an error: the handler responds with 503 until the frontend is built,
// keeping API-only development and Go tests usable.
func New(dist fs.FS) *Handler {
	handler := &Handler{dist: dist}
	if dist == nil {
		return handler
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err == nil {
		handler.index = index
		handler.built = true
	}
	return handler
}

// Built reports whether an index.html was found.
func (h *Handler) Built() bool {
	return h != nil && h.built
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.dist == nil || !h.built {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "frontend assets are not built", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name, ok := cleanAssetPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if name == "" || name == "index.html" {
		h.serveIndex(w, r)
		return
	}

	if data, err := fs.ReadFile(h.dist, name); err == nil {
		h.serveAsset(w, r, name, data)
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		http.Error(w, "frontend asset could not be read", http.StatusInternalServerError)
		return
	}

	// A missing hashed asset or any other file-with-extension must be a real
	// 404. Only route-like paths receive the SPA shell.
	if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
		http.NotFound(w, r)
		return
	}
	h.serveIndex(w, r)
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", SPAHeaderPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
	writeContent(w, r, h.index)
}

func (h *Handler) serveAsset(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(name, "assets/") {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else if name == "sw.js" || name == "theme-init.js" {
		header.Set("Cache-Control", "no-cache")
	} else {
		header.Set("Cache-Control", "public, max-age=86400")
	}
	writeContent(w, r, data)
}

func writeContent(w http.ResponseWriter, r *http.Request, data []byte) {
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}

// cleanAssetPath converts a URL path into a filesystem-relative slash path.
// Empty strings are the SPA root. Traversal attempts are rejected.
func cleanAssetPath(urlPath string) (string, bool) {
	if urlPath == "" || urlPath == "/" {
		return "", true
	}
	cleaned := path.Clean("/" + urlPath)
	if cleaned == "/" {
		return "", true
	}
	name := strings.TrimPrefix(cleaned, "/")
	if name == "" || !fs.ValidPath(name) {
		return "", false
	}
	if strings.Contains(name, "..") {
		return "", false
	}
	return name, true
}
