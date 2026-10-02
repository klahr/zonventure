// Package metrics holds the Prometheus collectors zv exposes when the
// operator turns metrics on. The collectors live on a private registry
// — not the global default — so a self-hoster who does not opt in
// gets a zero-cost no-op everywhere except inside this package.
//
// To enable the /metrics scrape endpoint, set ZV_METRICS_ADDR to a
// listen address (e.g. "127.0.0.1:9100" or ":9100"). When unset, no
// listener is created and the collectors are still safe to call —
// they just record into an unread registry. This matches the
// self-host design note: exposed-internals endpoints are off by
// default.
package metrics

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/klahr/zonventure/internal/logx"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is the package-private registry. Tests and the metrics
// listener both read it; nothing else should.
var Registry = prometheus.NewRegistry()

// HTTPRequests counts every served HTTP request. Labels are kept
// low-cardinality on purpose: status as a string ("2xx", "4xx", "5xx",
// "ws"), method, and a route template (chi.Router resolves the pattern
// before the handler runs, so e.g. "/api/admin/events/{eventID}" stays
// stable across all event ids).
var HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "zv_http_requests_total",
	Help: "HTTP requests handled by zv, labeled by method, status class, and route template.",
}, []string{"method", "status_class", "route"})

// HTTPDuration is request latency. Histogram with sane defaults for a
// web app; tune later if needed.
var HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "zv_http_request_duration_seconds",
	Help:    "HTTP request duration, labeled by method and route template.",
	Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
}, []string{"method", "route"})

// AuthLogins counts login attempts. Result is "success" or "fail" so
// alert rules can fire on a sustained fail rate without parsing log
// lines.
var AuthLogins = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "zv_auth_logins_total",
	Help: "Login attempts, labeled by outcome (success/fail).",
}, []string{"result"})

// PurchaseEvents counts purchase lifecycle transitions: "created",
// "confirmed_simulated", "confirmed_stripe", "refunded".
var PurchaseEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "zv_purchase_events_total",
	Help: "Purchase lifecycle events.",
}, []string{"event"})

func init() {
	Registry.MustRegister(
		HTTPRequests,
		HTTPDuration,
		AuthLogins,
		PurchaseEvents,
	)
}

// statusClass converts a numeric status into the low-cardinality label.
// Anything outside the standard 1xx-5xx range is bucketed as "other".
func statusClass(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500 && status < 600:
		return "5xx"
	default:
		return "other"
	}
}

// statusRecorder captures the response status so the middleware can
// label the counter without touching the handler. Mirrors the
// chi.middleware approach but keeps zero deps.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(s int) {
	if !r.wrote {
		r.status = s
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(s)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// Hijack proxies through to the underlying ResponseWriter so WebSocket
// upgrades work when this middleware is in the chain. Without this,
// coder/websocket.Accept fails the http.Hijacker assertion and writes
// 501 Not Implemented for every /ws/* endpoint.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("statusRecorder: underlying ResponseWriter is not a Hijacker")
	}
	if !r.wrote {
		r.status = http.StatusSwitchingProtocols
		r.wrote = true
	}
	return h.Hijack()
}

// Unwrap lets http.ResponseController reach the connection, so a
// handler can extend its read/write deadlines past the server's.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// HTTPMiddleware records request count + duration for every handler
// it wraps. Route comes from the chi route context — when the request
// didn't match a route (404), the label is "unmatched" rather than
// the raw URL, again to keep cardinality bounded.
func HTTPMiddleware(routeOf func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)
			elapsed := time.Since(start).Seconds()

			route := "unmatched"
			if routeOf != nil {
				if pattern := routeOf(r); pattern != "" {
					route = pattern
				}
			}
			HTTPRequests.WithLabelValues(r.Method, statusClass(rec.status), route).Inc()
			HTTPDuration.WithLabelValues(r.Method, route).Observe(elapsed)
		})
	}
}

// StatusString is a tiny helper for callers that already have a
// numeric status and want the bucketed label, exposed so the WS upgrade
// path can record without re-deriving.
func StatusString(status int) string { return statusClass(status) }

// ServeAddr starts a separate HTTP listener on addr serving GET
// /metrics. Returns immediately; the listener runs until ctx is
// cancelled. The listener exposes nothing else, so a misconfigured
// reverse proxy can't accidentally surface admin endpoints from the
// metrics port.
func ServeAddr(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		Registry: Registry,
	}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	logx.Info("metrics.listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logx.Error("metrics.listen.failed", "addr", addr, "err", err.Error())
	}
}
