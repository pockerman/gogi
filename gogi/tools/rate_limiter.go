package tools

import (
	"errors"
	"fmt"
	"gogi/gogi/storage/postgres"
	"sync"
	"time"
)

// ErrRateLimited is returned for a call that exceeds the rate limits of a tool
var ErrRateLimited = errors.New("rate limit exceeded")

// sessionCountTTL is how long the call count of a session is kept after its last call
const sessionCountTTL = 24 * time.Hour

type window struct {
	start time.Time
	count int32
}

type sessionCount struct {
	count    int32
	lastCall time.Time
}

// RateLimiter enforces the rate limits of tools: calls per minute and per day across
// all callers, and calls per session. The counts are kept in memory, so with several
// replicas of the service each replica enforces the limits separately
type RateLimiter struct {
	mu        sync.Mutex
	minutes   map[string]*window
	days      map[string]*window
	sessions  map[string]*sessionCount
	lastPrune time.Time
	now       func() time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		minutes:  make(map[string]*window),
		days:     make(map[string]*window),
		sessions: make(map[string]*sessionCount),
		now:      time.Now,
	}
}

// Allow records a call to the tool in the session, or returns ErrRateLimited if the
// call exceeds the tool's limits. A tool limited per session needs a session id
func (l *RateLimiter) Allow(tool *postgres.GogiTool, sessionID string) error {
	limits := tool.RateLimits
	if limits.RequestsPerMinute == 0 && limits.DailyLimit == 0 && limits.RequestsPerSession == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.prune(now)

	minute := l.current(l.minutes, tool.Name, now.Truncate(time.Minute))
	day := l.current(l.days, tool.Name, now.UTC().Truncate(24*time.Hour))
	sessionKey := tool.Name + "\x00" + sessionID
	session := l.sessions[sessionKey]

	switch {
	case limits.RequestsPerMinute > 0 && minute.count >= limits.RequestsPerMinute:
		return fmt.Errorf("%w: %s allows %d calls per minute", ErrRateLimited, tool.Name, limits.RequestsPerMinute)
	case limits.DailyLimit > 0 && day.count >= limits.DailyLimit:
		return fmt.Errorf("%w: %s allows %d calls per day", ErrRateLimited, tool.Name, limits.DailyLimit)
	case limits.RequestsPerSession > 0 && sessionID == "":
		return fmt.Errorf("%w: %s allows %d calls per session, so calls need a session id",
			ErrRateLimited, tool.Name, limits.RequestsPerSession)
	case limits.RequestsPerSession > 0 && session != nil && session.count >= limits.RequestsPerSession:
		return fmt.Errorf("%w: %s allows %d calls per session", ErrRateLimited, tool.Name, limits.RequestsPerSession)
	}

	minute.count++
	day.count++
	if limits.RequestsPerSession > 0 {
		if session == nil {
			session = &sessionCount{}
			l.sessions[sessionKey] = session
		}
		session.count++
		session.lastCall = now
	}
	return nil
}

// current returns the window of the tool that starts at start, starting a new one if needed
func (l *RateLimiter) current(windows map[string]*window, toolName string, start time.Time) *window {
	w, ok := windows[toolName]
	if !ok || !w.start.Equal(start) {
		w = &window{start: start}
		windows[toolName] = w
	}
	return w
}

// prune forgets the sessions without recent calls, at most once a minute
func (l *RateLimiter) prune(now time.Time) {
	if now.Sub(l.lastPrune) < time.Minute {
		return
	}
	l.lastPrune = now
	for key, session := range l.sessions {
		if now.Sub(session.lastCall) > sessionCountTTL {
			delete(l.sessions, key)
		}
	}
}
