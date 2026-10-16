// Package limits provides concurrency limiters for downloads and crawls.
package limits

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"tubarr/internal/domain/logger"

	"golang.org/x/net/publicsuffix"
)

// Per-domain limiters for video downloads and channel crawls.
var (
	DomainDownloads = &DomainLimiter{kind: "download", sems: make(map[string]chan struct{})}
	DomainCrawls    = &DomainLimiter{kind: "crawl", sems: make(map[string]chan struct{})}
)

// DomainLimiter limits concurrent operations per domain. A domain with no entry is unlimited.
type DomainLimiter struct {
	kind string
	mu   sync.RWMutex
	sems map[string]chan struct{}
}

// Init replaces all domain limits from a map.
func (l *DomainLimiter) Init(limits map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sems = make(map[string]chan struct{})
	for hostname, limit := range limits {
		if limit > 0 {
			l.sems[NormalizeDomain(hostname)] = make(chan struct{}, limit)
		}
	}
	logger.Pl.I("Initialized %d domain %s limit(s)", len(l.sems), l.kind)
}

// Set updates the limit for one domain at runtime (0 removes it). Holders of the old semaphore release cleanly.
func (l *DomainLimiter) Set(hostname string, limit int) {
	hostname = NormalizeDomain(hostname)
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit > 0 {
		l.sems[hostname] = make(chan struct{}, limit)
		logger.Pl.I("Domain %s limit for %q set to %d", l.kind, hostname, limit)
	} else {
		delete(l.sems, hostname)
		logger.Pl.I("Domain %s limit for %q removed (unlimited)", l.kind, hostname)
	}
}

// Acquire blocks until a slot is free for rawURL's domain, or ctx is done. Pass the returned semaphore to Release.
func (l *DomainLimiter) Acquire(ctx context.Context, rawURL string) (chan struct{}, error) {
	hostname := Hostname(rawURL)
	if hostname == "" {
		return nil, nil
	}
	l.mu.RLock()
	sem := l.sems[hostname]
	l.mu.RUnlock()
	if sem == nil {
		return nil, nil
	}
	if err := AcquireSlot(ctx, sem, hostname+" "+l.kind, rawURL); err != nil {
		return nil, err
	}
	return sem, nil
}

// AcquireSlot takes a slot on sem, logging if it has to wait for one.
func AcquireSlot(ctx context.Context, sem chan struct{}, limitDesc, target string) error {
	select {
	case sem <- struct{}{}:
		return nil
	default:
	}

	logger.Pl.I("Waiting for a free %s slot for %q (all %d in use)...", limitDesc, target, cap(sem))
	select {
	case sem <- struct{}{}:
		logger.Pl.D(1, "Got a %s slot for %q", limitDesc, target)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release releases a slot taken on sem (no-op if nil).
func Release(sem chan struct{}) {
	if sem != nil {
		<-sem
	}
}

// Hostname returns the eTLD+1 hostname of a URL (e.g. "youtube.com").
func Hostname(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return registeredDomain(parsed.Hostname())
}

// NormalizeDomain returns the eTLD+1 of a hostname or URL as entered by a user (e.g. "www.youtube.com" → "youtube.com").
func NormalizeDomain(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		return Hostname(s)
	}
	host, _, _ := strings.Cut(s, "/")
	return registeredDomain(host)
}

// registeredDomain returns the eTLD+1 of a hostname, or the lowercased hostname if it has none.
func registeredDomain(hostname string) string {
	if domain, err := publicsuffix.EffectiveTLDPlusOne(hostname); err == nil {
		return strings.ToLower(domain)
	}
	return strings.ToLower(hostname)
}
