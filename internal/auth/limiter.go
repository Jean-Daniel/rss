package auth

import (
	"sync"
	"time"
)

// Limiter slows down password guessing. Each address gets a few attempts per
// window; on top of that, all browsers that never logged in before share a
// global budget, so a distributed attack is capped too without locking the
// owner out of their known devices.
//
// Attempts are counted from the moment they start, not when the (slow)
// password check finishes, so sending many requests at once does not get
// more guesses through. State is in memory; a restart resets it.
type Limiter struct {
	mu        sync.Mutex
	window    time.Duration
	perIPMax  int
	globalMax int
	perIP     map[string]*attempts
	global    attempts
	now       func() time.Time
}

type attempts struct {
	failed  int
	pending int
	since   time.Time // start of the window for failed
}

func NewLimiter() *Limiter {
	return &Limiter{
		window:    15 * time.Minute,
		perIPMax:  5,
		globalMax: 20,
		perIP:     make(map[string]*attempts),
		now:       time.Now,
	}
}

// Attempt is a login attempt reserved with Begin. Exactly one of Succeeded,
// Failed or Release takes effect; later calls do nothing, so Release can be
// deferred.
type Attempt struct {
	l     *Limiter
	ip    string
	known bool
	done  bool
}

// Begin reserves an attempt for ip, or returns false when its limit (or, for
// unknown devices, the global limit) is used up, counting attempts that are
// still being checked.
func (l *Limiter) Begin(ip string, knownDevice bool) (*Attempt, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.expire(l.now())

	a := l.perIP[ip]
	if a != nil && a.failed+a.pending >= l.perIPMax {
		return nil, false
	}
	if !knownDevice && l.global.failed+l.global.pending >= l.globalMax {
		return nil, false
	}
	if a == nil {
		a = &attempts{}
		l.perIP[ip] = a
	}
	a.pending++
	if !knownDevice {
		l.global.pending++
	}
	return &Attempt{l: l, ip: ip, known: knownDevice}, true
}

// Succeeded clears the failures recorded for the attempt's address.
func (a *Attempt) Succeeded() { a.finish(func(p *attempts) { p.failed = 0 }, false) }

// Failed records a wrong password.
func (a *Attempt) Failed() {
	a.finish(func(p *attempts) { a.l.fail(p) }, true)
}

// Release ends the attempt without counting it, e.g. after a server error.
func (a *Attempt) Release() { a.finish(nil, false) }

func (a *Attempt) finish(update func(*attempts), failed bool) {
	l := a.l
	l.mu.Lock()
	defer l.mu.Unlock()
	if a.done {
		return
	}
	a.done = true

	p := l.perIP[a.ip] // never removed while it has pending attempts
	p.pending--
	if update != nil {
		update(p)
	}
	if !a.known {
		l.global.pending--
		if failed {
			l.fail(&l.global)
		}
	}
	if p.failed == 0 && p.pending == 0 {
		delete(l.perIP, a.ip)
	}
}

func (l *Limiter) fail(p *attempts) {
	if p.failed == 0 {
		p.since = l.now()
	}
	p.failed++
}

func (l *Limiter) expire(now time.Time) {
	if l.global.failed > 0 && now.Sub(l.global.since) >= l.window {
		l.global.failed = 0
	}
	for ip, p := range l.perIP {
		if p.failed > 0 && now.Sub(p.since) >= l.window {
			p.failed = 0
		}
		if p.failed == 0 && p.pending == 0 {
			delete(l.perIP, ip)
		}
	}
}
