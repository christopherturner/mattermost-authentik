package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	connectRateLimitMax       = 30
	connectGlobalRateLimitMax = 300
	connectRateLimitWindow    = time.Minute
	rateLimiterMaxBuckets     = 1000
)

type rateBucket struct {
	count       int
	windowStart time.Time
}

// ipRateLimiter is a thread-safe fixed-window per-IP rate limiter with lazy pruning.
type ipRateLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	now     func() time.Time
	buckets map[string]*rateBucket
}

func newIPRateLimiter(max int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		max:     max,
		window:  window,
		now:     time.Now,
		buckets: make(map[string]*rateBucket),
	}
}

// Allow reports whether a request from key is permitted under the rate limit.
func (l *ipRateLimiter) Allow(key string) bool {
	if l == nil || key == "" {
		return true
	}

	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.buckets) >= rateLimiterMaxBuckets {
		for k, b := range l.buckets {
			if now.Sub(b.windowStart) >= l.window {
				delete(l.buckets, k)
			}
		}
	}

	b, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= rateLimiterMaxBuckets {
			return false
		}
		l.buckets[key] = &rateBucket{
			count:       1,
			windowStart: now,
		}
		return true
	}

	if now.Sub(b.windowStart) >= l.window {
		b.count = 1
		b.windowStart = now
		return true
	}

	if b.count >= l.max {
		return false
	}

	b.count++
	return true
}

// getClientIP extracts the client IP address from proxy headers or RemoteAddr.
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		if ip := net.ParseIP(first); ip != nil {
			return ip.String()
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
