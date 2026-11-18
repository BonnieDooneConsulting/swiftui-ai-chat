package chat

import (
	"sync"
	"time"
)

// RateLimiter is an in-memory token bucket per key (the device ID).
//
// It's deliberately simple: state lives in one process and resets on restart.
// That's fine for a single instance. If you run several, move this to Redis
// or put the limit in your load balancer.
type RateLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens added per second
	burst   float64 // bucket size
	idleTTL time.Duration
	buckets map[string]*bucket
	now     func() time.Time
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter allows perMinute requests per key on average, with bursts of
// up to burst requests.
func NewRateLimiter(perMinute float64, burst int) *RateLimiter {
	if burst < 1 {
		burst = 1
	}
	return &RateLimiter{
		rate:    perMinute / 60.0,
		burst:   float64(burst),
		idleTTL: 30 * time.Minute,
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

// Allow reports whether key may make a request right now, and if not, roughly
// how long until it can.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.gc(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	if l.rate <= 0 {
		return false, time.Hour
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

// gc drops buckets that have been idle long enough to be full again, so the
// map doesn't grow forever. Caller holds l.mu.
func (l *RateLimiter) gc(now time.Time) {
	if now.Sub(l.lastGC) < time.Minute {
		return
	}
	l.lastGC = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > l.idleTTL {
			delete(l.buckets, k)
		}
	}
}
