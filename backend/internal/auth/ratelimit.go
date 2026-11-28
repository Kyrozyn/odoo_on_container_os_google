package auth

import (
	"sync"
	"time"
)

type attemptRecord struct {
	failures    int
	firstFailed time.Time
	lockedUntil time.Time
}

// LoginRateLimiter tracks failed login attempts per IP and enforces temporary lockouts.
type LoginRateLimiter struct {
	mu      sync.Mutex
	records map[string]*attemptRecord
}

func NewLoginRateLimiter() *LoginRateLimiter {
	return &LoginRateLimiter{
		records: make(map[string]*attemptRecord),
	}
}

func (l *LoginRateLimiter) Check(ip string) (allowed bool, retryAfterSec int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.records[ip]
	if !ok {
		return true, 0
	}
	now := time.Now()
	if now.Before(rec.lockedUntil) {
		sec := int(rec.lockedUntil.Sub(now).Seconds()) + 1
		return false, sec
	}
	return true, 0
}

func (l *LoginRateLimiter) RecordFailure(ip string, maxAttempts int, lockout time.Duration) (locked bool, retryAfterSec int) {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if lockout <= 0 {
		lockout = 10 * time.Minute
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	rec, ok := l.records[ip]
	if !ok || now.Sub(rec.firstFailed) > lockout {
		rec = &attemptRecord{firstFailed: now}
		l.records[ip] = rec
	}
	rec.failures++
	if rec.failures >= maxAttempts {
		rec.lockedUntil = now.Add(lockout)
		rec.failures = 0
		return true, int(lockout.Seconds())
	}
	return false, 0
}

func (l *LoginRateLimiter) RecordSuccess(ip string) {
	l.mu.Lock()
	delete(l.records, ip)
	l.mu.Unlock()
}
