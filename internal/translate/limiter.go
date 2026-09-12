package translate

import (
	"context"
	"sync"
	"time"
)

// A limiter spaces requests out so a run stays under a service's quota.
//
// Free tiers are usually capped in requests per minute, and the cap is the
// thing a long book runs into. Without one, Tulipe sends as fast as the
// service answers, collects a 429, waits, and sends again — which works, but
// pays for the lesson every time. Spacing the requests evenly avoids the 429
// instead of recovering from it.
//
// The slot is claimed before the wait, not after, so two callers never get the
// same one. Nothing is concurrent in Tulipe today; the limiter is written this
// way because request concurrency is the next thing to arrive, and a limiter
// that only works single-threaded would have to be rewritten the day it is
// finally needed.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	// next is when the following request may start. It runs ahead of the
	// clock as slots are handed out.
	next time.Time
}

// newLimiter spaces requests to at most perMinute of them. A value of zero or
// less means no limit, and returns nil — a nil limiter waits for nothing, so
// the caller has nothing to check.
func newLimiter(perMinute int) *limiter {
	if perMinute <= 0 {
		return nil
	}
	return &limiter{interval: time.Minute / time.Duration(perMinute)}
}

// reserve claims the next slot and says how long to wait for it.
func (l *limiter) reserve(now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.next.Before(now) {
		l.next = now
	}
	at := l.next
	l.next = at.Add(l.interval)
	return at.Sub(now)
}

// wait holds until this request's slot comes round. notify is called with the
// delay before waiting, and only when the wait is long enough to be worth
// explaining — a screen that sits still for thirty seconds without a word
// looks broken.
//
// A nil limiter waits for nothing.
func (l *limiter) wait(ctx context.Context, notify func(time.Duration)) error {
	if l == nil {
		return nil
	}
	delay := l.reserve(time.Now())
	if delay <= 0 {
		return nil
	}
	if notify != nil && delay >= noticeableWait {
		notify(delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// noticeableWait is the shortest pause worth telling the user about. Below it
// the message would flicker past without being read.
const noticeableWait = 2 * time.Second
