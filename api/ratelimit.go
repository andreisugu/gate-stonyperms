package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxRateLimitIPs    = 4096
	rateLimitStaleTTL  = 5 * time.Minute
	rateLimitSweepTick = time.Minute
)

type ipBucket struct {
	tokens   float64
	lastSeen time.Time
}

type ipRateLimiter struct {
	mu         sync.Mutex
	rps        float64
	burst      float64
	maxEntries int
	buckets    map[string]*ipBucket
	lastSweep  time.Time
	nowFn      func() time.Time
}

func newIPRateLimiter(rps float64, burst int) *ipRateLimiter {
	if rps <= 0 || burst <= 0 {
		return nil
	}
	now := time.Now()
	return &ipRateLimiter{
		rps:        rps,
		burst:      float64(burst),
		maxEntries: maxRateLimitIPs,
		buckets:    make(map[string]*ipBucket),
		lastSweep:  now,
		nowFn:      time.Now,
	}
}

func (l *ipRateLimiter) Allow(ip string) bool {
	if l == nil {
		return true
	}
	now := l.nowFn()

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= rateLimitSweepTick || len(l.buckets) >= l.maxEntries {
		l.evictStaleLocked(now)
	}

	b, ok := l.buckets[ip]
	if !ok {
		if len(l.buckets) >= l.maxEntries {
			l.evictOldestLocked()
		}
		l.buckets[ip] = &ipBucket{
			tokens:   l.burst - 1,
			lastSeen: now,
		}
		return true
	}

	if dt := now.Sub(b.lastSeen).Seconds(); dt > 0 {
		b.tokens += dt * l.rps
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
	}
	b.lastSeen = now
	if b.tokens < 1.0 {
		return false
	}
	b.tokens -= 1.0
	return true
}

func (l *ipRateLimiter) evictStaleLocked(now time.Time) {
	l.lastSweep = now
	for ip, b := range l.buckets {
		if now.Sub(b.lastSeen) > rateLimitStaleTTL {
			delete(l.buckets, ip)
		}
	}
}

func (l *ipRateLimiter) evictOldestLocked() {
	var oldestIP string
	var oldestTime time.Time
	first := true
	for ip, b := range l.buckets {
		if first || b.lastSeen.Before(oldestTime) {
			oldestIP = ip
			oldestTime = b.lastSeen
			first = false
		}
	}
	if !first {
		delete(l.buckets, oldestIP)
	}
}

// clientIP extracts the network peer IP from r.RemoteAddr, ignoring unverified
// forwarding headers such as X-Forwarded-For.
func clientIP(r *http.Request) string {
	raw := strings.TrimSpace(r.RemoteAddr)
	if raw == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(raw); err == nil && host != "" {
		return host
	}
	return raw
}

func (s *Server) rateLimit(lim *ipRateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !lim.Allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded; retry shortly")
			return
		}
		next.ServeHTTP(w, r)
	})
}
