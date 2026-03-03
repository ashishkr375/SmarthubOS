package handlers

import (
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// ingestLimiters maps device_id string → *rate.Limiter for per-device rate limiting.
// Each device is limited to 100 events/second burst 200 to prevent abuse.
var (
	ingestLimiters sync.Map
	ingestRate     = rate.Limit(100)  // events per second
	ingestBurst    = 200
)

// IngestRateLimiter returns a chi-compatible middleware that enforces per-device
// rate limits on the ingest endpoint.  The device_id is read from the first item
// in the JSON body via the X-Device-ID request header (hub must set this).
//
// Hubs should set the X-Device-ID header to the primary device being ingested.
// If the header is absent, a shared "unknown" limiter is used.
func IngestRateLimiter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Device-ID")
		if key == "" {
			key = "_unknown_"
		}

		actual, _ := ingestLimiters.LoadOrStore(key, rate.NewLimiter(ingestRate, ingestBurst))
		limiter := actual.(*rate.Limiter)

		if !limiter.Allow() {
			writeError(w, http.StatusTooManyRequests, "rate_limit_exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
