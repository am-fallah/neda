package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/logging"
)

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := logging.NewRequestID()
		sw, ok := w.(*statusWriter)
		if !ok {
			sw = &statusWriter{ResponseWriter: w, status: http.StatusOK}
		}
		sw.Header().Set(requestIDHeader, id)
		next.ServeHTTP(sw, r.WithContext(logging.WithRequestID(r.Context(), id)))
	})
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)

		status := http.StatusOK
		if sw, ok := w.(*statusWriter); ok {
			status = sw.status
		}
		attrs := []any{
			"request_id", logging.RequestID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
		}
		if status >= http.StatusInternalServerError {
			s.logger.Error("http request", attrs...)
		} else {
			s.logger.Info("http request", attrs...)
		}
	})
}

func (s *Server) recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic recovered",
					"request_id", logging.RequestID(r.Context()),
					"panic", logging.RedactString(fmt.Sprint(recovered)),
				)
				if sw, ok := w.(*statusWriter); ok {
					if !sw.wrote {
						writeError(sw, r, http.StatusInternalServerError, "internal_error", "internal server error")
					}
					return
				}
				writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set(headerContentTypeOption, "nosniff")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Cross-Origin-Opener-Policy", "same-origin")
		headers.Set("Cross-Origin-Resource-Policy", "same-origin")
		headers.Set("X-Permitted-Cross-Domain-Policies", "none")
		headers.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		headers.Set(headerCacheControl, "no-store")
		if s.cfg.Production && s.cfg.PublicURL != nil && strings.EqualFold(s.cfg.PublicURL.Scheme, "https") {
			headers.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostValidation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requestHostMatches(r.Host) {
			writeError(w, r, http.StatusMisdirectedRequest, "misdirected_request", "request host is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > s.cfg.MaxBodyBytes {
			writeError(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "request body is too large")
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
