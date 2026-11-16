package controller

import (
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"
)

// tokenLimiter counts each token's requests in fixed one-minute windows.
type tokenLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	windows map[int]*tokenWindow
}

type tokenWindow struct {
	start time.Time
	count int
}

var defaultTokenLimiter = &tokenLimiter{now: time.Now, windows: map[int]*tokenWindow{}}

func (l *tokenLimiter) allow(tokenID, perMinute int) bool {
	if perMinute <= 0 {
		perMinute = panel.DefaultTokenRateLimit
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	w := l.windows[tokenID]
	if w == nil || now.Sub(w.start) >= time.Minute {
		l.windows[tokenID] = &tokenWindow{start: now, count: 1}
		return true
	}
	if w.count >= perMinute {
		return false
	}
	w.count++
	return true
}
