package auth

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter membatasi percobaan per kunci (mis. IP) dengan token bucket.
//
// Penyimpanan in-memory: state hilang saat restart dan tidak dibagi antar
// instance. Cukup untuk demo satu instance; untuk multi-instance gunakan
// penyimpanan bersama (mis. PostgreSQL/Redis).
type RateLimiter struct {
	mu       sync.Mutex
	limit    rate.Limit
	burst    int
	ttl      time.Duration
	now      func() time.Time
	entries  map[string]*limiterEntry
	lastSwep time.Time
}

type limiterEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter: setiap kunci boleh `burst` percobaan sekaligus, lalu
// diisi ulang satu token setiap `every`.
func NewRateLimiter(every time.Duration, burst int) *RateLimiter {
	return &RateLimiter{
		limit:   rate.Every(every),
		burst:   burst,
		ttl:     every * time.Duration(burst) * 2,
		now:     time.Now,
		entries: make(map[string]*limiterEntry),
	}
}

// Allow mengonsumsi satu token untuk key dan melaporkan apakah diizinkan.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	e, ok := l.entries[key]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(l.limit, l.burst)}
		l.entries[key] = e
	}
	e.lastSeen = now
	return e.lim.AllowN(now, 1)
}

// sweep membuang entri lama agar map tidak tumbuh tanpa batas.
func (l *RateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSwep) < time.Minute {
		return
	}
	l.lastSwep = now
	for k, e := range l.entries {
		if now.Sub(e.lastSeen) > l.ttl {
			delete(l.entries, k)
		}
	}
}

func (l *RateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}
