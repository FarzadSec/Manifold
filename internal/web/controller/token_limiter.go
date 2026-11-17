package controller

import (
	"sync"
	"time"
)

// tokenLimiter counts each token's requests in fixed one-minute windows.
type tokenLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	windows map[string]*tokenWindow
}

type tokenWindow struct {
	start time.Time
	count int
}

var defaultTokenLimiter = &tokenLimiter{now: time.Now, windows: map[string]*tokenWindow{}}

// allow is keyed by the token's hash, which no other token can ever share, unlike a row id.
// A token that names no rate is never throttled.
func (l *tokenLimiter) allow(tokenHash string, perMinute int) bool {
	if perMinute <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.windows[tokenHash]
	if w == nil || now.Sub(w.start) >= time.Minute {
		l.windows[tokenHash] = &tokenWindow{start: now, count: 1}
		return true
	}
	if w.count >= perMinute {
		return false
	}
	w.count++
	return true
}
