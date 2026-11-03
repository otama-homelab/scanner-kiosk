package apiruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"scanner-kiosk/internal/config"
	"strconv"
	"strings"
	"sync"
	"time"
)

// bounded applies per-process rate/concurrency bounds without trusting client
// address/forwarding headers. Health probes are independent from user traffic.
func bounded(next http.Handler, maxInflight int, rate, burst float64, transferTimeout time.Duration) http.Handler {
	slots := make(chan struct{}, maxInflight)
	var mu sync.Mutex
	tokens := burst
	last := time.Now()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			mu.Lock()
			now := time.Now()
			tokens = min(burst, tokens+now.Sub(last).Seconds()*rate)
			last = now
			allowed := tokens >= 1
			retry := max(1, int(math.Ceil((1-tokens)/rate)))
			if allowed {
				tokens--
			}
			mu.Unlock()
			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				reject(w, 429, "rate_limited", "Too many requests")
				return
			}
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			reject(w, 503, "busy", "API busy")
			return
		}
		timeout := 30 * time.Second
		if (r.Method == "GET" || r.Method == "HEAD") && strings.HasPrefix(r.URL.Path, "/api/documents/") && (strings.HasSuffix(r.URL.Path, "/download") || strings.HasSuffix(r.URL.Path, "/preview")) {
			timeout = transferTimeout
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		next.ServeHTTP(w, r)
	})
}

// Run serves an HTTP backend for a trusted TLS reverse proxy which preserves
// configured Host. It never weakens Secure Cookies or falls back from OIDC.
func Run(ctx context.Context, c config.Config) error {
	app, err := New(ctx, c)
	if err != nil {
		return err
	}
	defer app.Close()
	listener, err := net.Listen("tcp", c.ListenAddr)
	if err != nil {
		return ErrListen
	}
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, BaseContext: func(net.Listener) context.Context { return ctx }, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return ErrListen
	case <-ctx.Done():
		app.closed.Store(true)
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
		}
		<-done
		return ctx.Err()
	}
}

func reject(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
