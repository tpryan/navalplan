package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tpryan/navalplan/services/researcher/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

var _ http.ResponseWriter = (*responseWriter)(nil)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		defer func() {
			timesince := time.Since(start)
			str := timesince.String()

			level := slog.LevelInfo
			if ww.statusCode >= 400 {
				level = slog.LevelWarn
			}
			if ww.statusCode >= 500 {
				level = slog.LevelError
			}

			slog.Log(r.Context(), level, "Request handled",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.statusCode,
				"duration", str,
				"remote_addr", r.RemoteAddr,
			)
		}()

		next.ServeHTTP(ww, r)
	})
}

func traceMiddleware(projectID string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		traceHeader := r.Header.Get("X-Cloud-Trace-Context")
		if traceHeader != "" {
			parts := strings.Split(traceHeader, ";")
			if len(parts) > 0 {
				traceParts := strings.Split(parts[0], "/")
				if len(traceParts) > 0 && len(traceParts[0]) > 0 {
					traceID := traceParts[0]
					var traceStr string
					if projectID != "" {
						traceStr = fmt.Sprintf("projects/%s/traces/%s", projectID, traceID)
					} else {
						traceStr = traceID
					}
					ctx = telemetry.AddTraceToContext(ctx, traceStr)

					if len(traceParts) > 1 {
						ctx = telemetry.AddSpanToContext(ctx, traceParts[1])
					}
				}
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
