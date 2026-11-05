// Package limits provides concurrency limiters for downloads, crawls and post-processing.
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
	DomainDownloads = &DomainLimiter{kind: "download", sems: make(map[string]*Semaphore)}
	DomainCrawls    = &DomainLimiter{kind: "crawl", sems: make(map[string]*Semaphore)}
)

// GlobalPostProcessing limits Metarr runs across all channels.
var GlobalPostProcessing = NewSemaphore(0)

// InitGlobalPostProcessLimit sets the Metarr concurrency limit across all channels (0 = unlimited).
func InitGlobalPostProcessLimit(limit int) {
	GlobalPostProcessing.SetLimit(limit)
	if limit > 0 {
		logger.Pl.I("Global post-processing concurrency limit set to %d", limit)
	} else {
		logger.Pl.I("Global post-processing concurrency limit disabled (unlimited)")
	}
}

// DomainLimiter limits concurrent operations per domain. A domain with no limit is unlimited.
type DomainLimiter struct {
	kind string // The type of thing being limited (e.g. downloads, or crawls)
	mu   sync.Mutex
	sems map[string]*Semaphore
}

// Init replaces all domain limits from a map, at startup before anything takes a slot.
func (l *DomainLimiter) Init(limits map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sems = make(map[string]*Semaphore)
	for hostname, limit := range limits {
		if limit > 0 {
			l.sems[NormalizeDomain(hostname)] = NewSemaphore(limit)
		}
	}
	logger.Pl.I("Initialized %d domain %s limit(s)", len(l.sems), l.kind)
}

// Set updates the limit for one domain at runtime (0 removes it). Waiters get slots the new limit frees straight away.
func (l *DomainLimiter) Set(hostname string, limit int) {
	hostname = NormalizeDomain(hostname)
	l.semaphore(hostname).SetLimit(limit)
	if limit > 0 {
		logger.Pl.I("Domain %s limit for %q set to %d", l.kind, hostname, limit)
	} else {
		logger.Pl.I("Domain %s limit for %q removed (unlimited)", l.kind, hostname)
	}
}

// Acquire blocks until a slot is free for rawURL's domain, or ctx is done. Call Release on the returned semaphore.
func (l *DomainLimiter) Acquire(ctx context.Context, rawURL string) (*Semaphore, error) {
	// Each hostname can have a different concurrency limit.
	hostname := Hostname(rawURL)
	sem := l.semaphore(hostname)

	// Try to acquire a slot immediately without waiting.
	if sem.TryAcquire() {
		return sem, nil
	}

	// No slots were available:
	logger.Pl.I("Waiting for a free %s %s slot for %q...", hostname, l.kind, rawURL)
	if err := sem.Acquire(ctx); err != nil {
		return nil, err
	}
	logger.Pl.D(1, "Got a %s %s slot for %q", hostname, l.kind, rawURL)
	return sem, nil
}

// semaphore returns hostname's semaphore, adding an unlimited one so slots are counted if a limit is set later.
func (l *DomainLimiter) semaphore(hostname string) *Semaphore {
	l.mu.Lock()
	defer l.mu.Unlock()
	sem, exists := l.sems[hostname]
	if !exists {
		sem = NewSemaphore(0)
		l.sems[hostname] = sem
	}
	return sem
}

// AcquireSlot takes a slot on sem, logging if it has to wait for one.
func AcquireSlot(ctx context.Context, sem chan struct{}, limitDesc, target string) error {
	// Try to acquire a slot immediately without waiting.
	select {
	case sem <- struct{}{}:
		return nil
	default:
		// Continue below.
	}

	// No slots were available:
	logger.Pl.I("Waiting for a free %s slot for %q (all %d in use)...", limitDesc, target, cap(sem))
	select {
	case sem <- struct{}{}: // Slot got released, can continue.
		logger.Pl.D(1, "Got a %s slot for %q", limitDesc, target)
		return nil
	case <-ctx.Done(): // Context was cancelled or its deadline expired, so abort.
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
	return RegisteredDomain(parsed.Hostname())
}

// NormalizeDomain returns the eTLD+1 of a hostname or URL as entered by a user (e.g. "www.youtube.com" → "youtube.com").
func NormalizeDomain(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		return Hostname(s)
	}
	host, _, _ := strings.Cut(s, "/")
	return RegisteredDomain(host)
}

// RegisteredDomain returns the eTLD+1 of a hostname, or the lowercased hostname if it has none.
//
// Lowercases first, as the public suffix list is lowercase ("EXAMPLE.CO.UK" would otherwise give "co.uk").
func RegisteredDomain(hostname string) string {
	hostname = strings.ToLower(hostname)
	if domain, err := publicsuffix.EffectiveTLDPlusOne(hostname); err == nil {
		return domain
	}
	return hostname
}
