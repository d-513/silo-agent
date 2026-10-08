package auth

import (
	"sync"
	"time"
)

// Limiter slows down guessing at a sign-in. It counts failures three ways:
//
//   - per account and address together: five wrong tries, then a wait that
//     doubles from 30 seconds to 15 minutes. This is the one a person who
//     forgot their password meets, and it holds up nobody else.
//   - per address: 20 failures in 15 minutes, for one machine trying many
//     accounts.
//   - per account: 50 failures in an hour, for many machines trying one.
//
// Counts live in memory, so a restart clears them.
type Limiter struct {
	mu       sync.Mutex
	now      func() time.Time
	pairs    map[string]*pairCount
	addrs    map[string][]time.Time
	accounts map[string][]time.Time
	pruned   time.Time
}

const (
	pairFree    = 4
	pairFirst   = 30 * time.Second
	pairMax     = 15 * time.Minute
	pairIdle    = time.Hour
	addrLimit   = 20
	addrWindow  = 15 * time.Minute
	acctLimit   = 50
	acctWindow  = time.Hour
	pruneEvery  = 10 * time.Minute
	limiterNoIP = "-"
)

type pairCount struct {
	fails int
	last  time.Time
	until time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{now: time.Now, pairs: map[string]*pairCount{}, addrs: map[string][]time.Time{}, accounts: map[string][]time.Time{}}
}

// Wait is how long this account at this address must wait before another try;
// zero means go ahead. account may be empty when there is none to name.
func (l *Limiter) Wait(account, addr string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var wait time.Duration
	if p := l.pairs[pairKey(account, addr)]; p != nil && account != "" {
		wait = max(wait, p.until.Sub(now))
	}
	wait = max(wait, windowWait(l.addrs[addrKey(addr)], now, addrLimit, addrWindow))
	if account != "" {
		wait = max(wait, windowWait(l.accounts[account], now, acctLimit, acctWindow))
	}
	return max(wait, 0)
}

// Fail records one wrong try.
func (l *Limiter) Fail(account, addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.pruned) >= pruneEvery {
		l.pruned = now
		l.prune(now)
	}
	l.addrs[addrKey(addr)] = append(fresh(l.addrs[addrKey(addr)], now, addrWindow), now)
	if account == "" {
		return
	}
	l.accounts[account] = append(fresh(l.accounts[account], now, acctWindow), now)
	k := pairKey(account, addr)
	p := l.pairs[k]
	if p == nil || now.Sub(p.last) > pairIdle {
		p = &pairCount{}
		l.pairs[k] = p
	}
	p.fails++
	p.last = now
	if over := p.fails - pairFree; over > 0 {
		wait := pairMax
		if over <= 6 {
			wait = min(pairFirst<<(over-1), pairMax)
		}
		p.until = now.Add(wait)
	}
}

// OK records a right try: the account's count at this address starts over.
func (l *Limiter) OK(account, addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.pairs, pairKey(account, addr))
}

func (l *Limiter) prune(now time.Time) {
	for k, p := range l.pairs {
		if now.Sub(p.last) > pairIdle && !p.until.After(now) {
			delete(l.pairs, k)
		}
	}
	for k, v := range l.addrs {
		if len(fresh(v, now, addrWindow)) == 0 {
			delete(l.addrs, k)
		}
	}
	for k, v := range l.accounts {
		if len(fresh(v, now, acctWindow)) == 0 {
			delete(l.accounts, k)
		}
	}
}

func pairKey(account, addr string) string { return account + "|" + addrKey(addr) }

func addrKey(addr string) string {
	if addr == "" {
		return limiterNoIP
	}
	return addr
}

// fresh drops the times that have left the window.
func fresh(times []time.Time, now time.Time, window time.Duration) []time.Time {
	i := 0
	for i < len(times) && now.Sub(times[i]) >= window {
		i++
	}
	return times[i:]
}

// windowWait is how long until fewer than limit of times are inside the window.
func windowWait(times []time.Time, now time.Time, limit int, window time.Duration) time.Duration {
	times = fresh(times, now, window)
	if len(times) < limit {
		return 0
	}
	return times[len(times)-limit].Add(window).Sub(now)
}
