package auth

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }
func newFakeClock() *fakeClock           { return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }
func limiterWith(c *fakeClock, every time.Duration, burst int) *RateLimiter {
	l := NewRateLimiter(every, burst)
	l.now = c.now
	return l
}

func TestRateLimiterBurstThenBlock(t *testing.T) {
	c := newFakeClock()
	l := limiterWith(c, 12*time.Second, 5)
	for i := 0; i < 5; i++ {
		if !l.Allow("10.0.0.1") {
			t.Fatalf("percobaan ke-%d seharusnya diizinkan", i+1)
		}
	}
	if l.Allow("10.0.0.1") {
		t.Fatal("percobaan ke-6 seharusnya diblokir")
	}
	c.add(12 * time.Second)
	if !l.Allow("10.0.0.1") {
		t.Fatal("setelah token terisi ulang seharusnya diizinkan lagi")
	}
	if l.Allow("10.0.0.1") {
		t.Fatal("hanya satu token yang terisi ulang")
	}
}

func TestRateLimiterIsPerKey(t *testing.T) {
	c := newFakeClock()
	l := limiterWith(c, time.Minute, 1)
	if !l.Allow("a") || l.Allow("a") {
		t.Fatal("kunci a: 1 diizinkan lalu diblokir")
	}
	if !l.Allow("b") {
		t.Fatal("kunci b tidak boleh terpengaruh kunci a")
	}
}

func TestRateLimiterSweepsStaleEntries(t *testing.T) {
	c := newFakeClock()
	l := limiterWith(c, time.Second, 1)
	l.Allow("a")
	l.Allow("b")
	c.add(time.Hour)
	l.Allow("c")
	if got := l.size(); got != 1 {
		t.Fatalf("entri lama harus dibuang, sisa %d", got)
	}
}
