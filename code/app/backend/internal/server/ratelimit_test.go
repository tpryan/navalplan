package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"app/internal/config"

	"github.com/stretchr/testify/assert"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{ContentDir: "."}
	srv, err := New(nil, cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	return srv
}

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

var ipCounter atomic.Int64

func uniqueIP() string {
	n := ipCounter.Add(1)
	return fmt.Sprintf("192.0.2.%d:1234", n%254+1)
}

func TestRateLimit_AllowsUpToLimit(t *testing.T) {
	srv := newTestServer(t)
	limit := 3
	handler := srv.rateLimit(limit, time.Minute)(okHandler)
	ip := uniqueIP()

	for i := 1; i <= limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should be allowed", i)
	}
}

func TestRateLimit_BlocksOverLimit(t *testing.T) {
	srv := newTestServer(t)
	limit := 3
	handler := srv.rateLimit(limit, time.Minute)(okHandler)
	ip := uniqueIP()

	for i := 1; i <= limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "request over limit should be rejected")
}

func TestRateLimit_ResetsAfterWindow(t *testing.T) {
	srv := newTestServer(t)
	limit := 2
	window := 50 * time.Millisecond
	handler := srv.rateLimit(limit, window)(okHandler)
	ip := uniqueIP()

	for i := 0; i < limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	time.Sleep(window + 10*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "request after window reset should be allowed")
}

func TestRateLimit_IsolatesByKey(t *testing.T) {
	srv := newTestServer(t)
	limit := 1
	handler := srv.rateLimit(limit, time.Minute)(okHandler)

	ip1 := uniqueIP()
	ip2 := uniqueIP()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip1
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip1
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code, "ip1 should be limited")

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip2
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "ip2 should not be affected by ip1's limit")
}

func TestRateLimit_ConcurrentAccess(t *testing.T) {
	srv := newTestServer(t)
	limit := 100
	handler := srv.rateLimit(limit, time.Minute)(okHandler)
	ip := uniqueIP()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = ip
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			code := w.Code
			if code != http.StatusOK && code != http.StatusTooManyRequests {
				t.Errorf("unexpected status code: %d", code)
			}
		}()
	}

	wg.Wait()
}
