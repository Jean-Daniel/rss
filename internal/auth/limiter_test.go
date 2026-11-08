package auth

import (
	"sync"
	"testing"
	"time"
)

func newTestLimiter() (*Limiter, *time.Time) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	l := NewLimiter()
	l.now = func() time.Time { return now }
	return l, &now
}

func TestLimiterCountsAttemptsInFlight(t *testing.T) {
	l, _ := newTestLimiter()
	// Many simultaneous requests from one address: only perIPMax may start.
	var mu sync.Mutex
	var started []*Attempt
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if a, ok := l.Begin("198.51.100.7", false); ok {
				mu.Lock()
				started = append(started, a)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(started) != l.perIPMax {
		t.Fatalf("%d attempts started at once, want %d", len(started), l.perIPMax)
	}
	for _, a := range started {
		a.Failed()
	}
	if _, ok := l.Begin("198.51.100.7", false); ok {
		t.Fatal("attempt allowed after using up the per-address limit")
	}
}

func TestLimiterGlobalLimitSparesKnownDevices(t *testing.T) {
	l, _ := newTestLimiter()
	// A distributed attack: one wrong password from each of many addresses.
	for i := range l.globalMax {
		a, ok := l.Begin(string(rune('A'+i)), false)
		if !ok {
			t.Fatalf("attempt %d refused before the global limit", i)
		}
		a.Failed()
	}
	if _, ok := l.Begin("203.0.113.1", false); ok {
		t.Fatal("unknown browser allowed past the global limit")
	}
	a, ok := l.Begin("203.0.113.1", true)
	if !ok {
		t.Fatal("known browser refused because of the global limit")
	}
	a.Succeeded()
}

func TestLimiterWindowExpires(t *testing.T) {
	l, now := newTestLimiter()
	for range l.perIPMax {
		a, _ := l.Begin("192.0.2.1", false)
		a.Failed()
	}
	if _, ok := l.Begin("192.0.2.1", false); ok {
		t.Fatal("allowed while limited")
	}
	*now = now.Add(l.window)
	a, ok := l.Begin("192.0.2.1", false)
	if !ok {
		t.Fatal("still limited after the window")
	}
	a.Release()
	if len(l.perIP) != 0 || l.global.pending != 0 {
		t.Fatalf("state left behind: perIP=%d globalPending=%d", len(l.perIP), l.global.pending)
	}
}

func TestLimiterReleaseAndSuccess(t *testing.T) {
	l, _ := newTestLimiter()
	for range 3 {
		a, _ := l.Begin("192.0.2.2", false)
		a.Failed()
	}
	a, _ := l.Begin("192.0.2.2", false)
	a.Succeeded()
	a.Failed() // ignored: already finished
	a.Release()
	if _, ok := l.perIP["192.0.2.2"]; ok {
		t.Fatal("a successful login should clear the address's failures")
	}
	if l.global.failed != 3 {
		t.Fatalf("global failures = %d, want 3", l.global.failed)
	}
}

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"abcdé", 5, "abcd"}, // é is 2 bytes and would be split
		{"abcdé", 6, "abcdé"},
		{"a\xffb", 10, "ab"}, // invalid bytes dropped
	}
	for _, tt := range tests {
		if got := truncateUTF8(tt.in, tt.max); got != tt.want {
			t.Errorf("truncateUTF8(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}
