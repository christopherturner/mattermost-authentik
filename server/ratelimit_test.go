package main

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIPRateLimiter(t *testing.T) {
	t.Run("allows up to max and blocks excess within window", func(t *testing.T) {
		now := time.Unix(1000, 0)
		limiter := newIPRateLimiter(3, time.Minute)
		limiter.now = func() time.Time { return now }

		assert.True(t, limiter.Allow("192.0.2.1"))
		assert.True(t, limiter.Allow("192.0.2.1"))
		assert.True(t, limiter.Allow("192.0.2.1"))
		assert.False(t, limiter.Allow("192.0.2.1"))

		// Different IP is unaffected
		assert.True(t, limiter.Allow("192.0.2.2"))

		// Advance past window -> resets
		now = now.Add(time.Minute)
		assert.True(t, limiter.Allow("192.0.2.1"))
	})

	t.Run("prunes expired buckets when exceeding maxBuckets", func(t *testing.T) {
		now := time.Unix(1000, 0)
		limiter := newIPRateLimiter(5, time.Minute)
		limiter.now = func() time.Time { return now }

		for i := 0; i < rateLimiterMaxBuckets; i++ {
			assert.True(t, limiter.Allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256)))
		}
		assert.Equal(t, rateLimiterMaxBuckets, len(limiter.buckets))
		// Capacity reached within window -> new key rejected
		assert.False(t, limiter.Allow("203.0.113.99"))

		now = now.Add(2 * time.Minute)
		assert.True(t, limiter.Allow("203.0.113.1"))
		assert.Equal(t, 1, len(limiter.buckets))
	})

	t.Run("handles nil limiter and empty key", func(t *testing.T) {
		var nilLimiter *ipRateLimiter
		assert.True(t, nilLimiter.Allow("192.0.2.1"))

		limiter := newIPRateLimiter(1, time.Minute)
		assert.True(t, limiter.Allow(""))
	})
}

func TestGetClientIP(t *testing.T) {
	t.Run("prefers valid X-Forwarded-For first entry", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.1")
		req.Header.Set("X-Real-IP", "198.51.100.20")
		req.RemoteAddr = "127.0.0.1:12345"
		assert.Equal(t, "198.51.100.10", getClientIP(req))
	})

	t.Run("falls back to X-Real-IP when XFF invalid or absent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Forwarded-For", "not-an-ip")
		req.Header.Set("X-Real-IP", "198.51.100.20")
		req.RemoteAddr = "127.0.0.1:12345"
		assert.Equal(t, "198.51.100.20", getClientIP(req))
	})

	t.Run("falls back to RemoteAddr host", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.0.2.55:54321"
		assert.Equal(t, "192.0.2.55", getClientIP(req))
	})
}
