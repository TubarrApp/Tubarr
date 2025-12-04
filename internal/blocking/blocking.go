// Package blocking handles global domain blocking logic for bot detection.
package blocking

import (
	"database/sql"
	"fmt"
	"maps"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/models"

	"golang.org/x/net/publicsuffix"
)

// GetBlockContext determines the block context for a channel URL based on authentication settings.
func GetBlockContext(cu *models.ChannelURL) vars.BlockContext {
	// Priority #1: Username/password authentication.
	if cu != nil && cu.LoginURL != "" && cu.Username != "" {
		return vars.BlockContextAuth
	}

	// Priority #2: Cookie-based authentication.
	if cu.ChanURLSettings != nil && (cu.ChanURLSettings.CookiesFromBrowser != "" || cu.ChanURLSettings.UseGlobalCookies) {
		return vars.BlockContextCookie
	}

	// Priority #3: Unauthenticated.
	return vars.BlockContextUnauth
}

// BlockDomain blocks a domain for a specific context globally (memory + database).
func BlockDomain(db *sql.DB, domain string, context vars.BlockContext) error {
	now := time.Now()

	// Normalize domain to eTLD+1.
	normalizedDomain := normalizeDomain(domain)

	// Update in-memory state.
	vars.BlockedDomainsMutex.Lock()
	defer vars.BlockedDomainsMutex.Unlock()

	if vars.BlockedDomains == nil {
		vars.BlockedDomains = make(map[string]map[vars.BlockContext]time.Time)
	}
	if vars.BlockedDomains[normalizedDomain] == nil {
		vars.BlockedDomains[normalizedDomain] = make(map[vars.BlockContext]time.Time)
	}
	vars.BlockedDomains[normalizedDomain][context] = now

	// Persist to database.
	query := fmt.Sprintf(`INSERT OR REPLACE INTO %s (%s, %s, %s) VALUES (?, ?, ?)`,
		consts.DBBlockedDomains,
		consts.QBlockedDomain,
		consts.QBlockedContext,
		consts.QBlockedAt,
	)

	if _, err := db.Exec(query, normalizedDomain, string(context), now); err != nil {
		return fmt.Errorf("failed to persist blocked domain %q (context: %s) to database: %w", normalizedDomain, context, err)
	}

	logger.Pl.W("Blocked domain %q for context %q due to bot detection", normalizedDomain, context)
	return nil
}

// IsBlocked checks if a domain is blocked for a specific context.
//
// Returns true if blocked and timeout has not expired.
func IsBlocked(domain string, context vars.BlockContext) (isBlocked bool, blockedAt time.Time, remainingTime time.Duration) {
	normalizedDomain := normalizeDomain(domain)

	vars.BlockedDomainsMutex.RLock()
	defer vars.BlockedDomainsMutex.RUnlock()

	if vars.BlockedDomains == nil || vars.BlockedDomains[normalizedDomain] == nil {
		return false, time.Time{}, 0
	}

	blockedTime, exists := vars.BlockedDomains[normalizedDomain][context]
	if !exists {
		return false, time.Time{}, 0
	}

	// Check if timeout has expired.
	timeoutMinutes := GetTimeoutForDomain(normalizedDomain)
	timeout := time.Duration(timeoutMinutes) * time.Minute
	unlockTime := blockedTime.Add(timeout)

	if time.Now().After(unlockTime) {
		// Timeout expired, not blocked anymore.
		return false, blockedTime, 0
	}

	remaining := time.Until(unlockTime)
	return true, blockedTime, remaining
}

// UnblockDomain unblocks a domain for a specific context (or all contexts if context is empty).
func UnblockDomain(db *sql.DB, domain string, context vars.BlockContext) error {
	normalizedDomain := normalizeDomain(domain)

	vars.BlockedDomainsMutex.Lock()
	defer vars.BlockedDomainsMutex.Unlock()

	if context == "" {
		// Unblock all contexts for this domain.
		delete(vars.BlockedDomains, normalizedDomain)

		query := fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`,
			consts.DBBlockedDomains,
			consts.QBlockedDomain,
		)

		if _, err := db.Exec(query, normalizedDomain); err != nil {
			return fmt.Errorf("failed to unblock domain %q from database: %w", normalizedDomain, err)
		}

		logger.Pl.S("Unblocked domain %q for all contexts", normalizedDomain)
	} else {
		// Unblock specific context.
		if vars.BlockedDomains[normalizedDomain] != nil {
			delete(vars.BlockedDomains[normalizedDomain], context)
			if len(vars.BlockedDomains[normalizedDomain]) == 0 {
				delete(vars.BlockedDomains, normalizedDomain)
			}
		}

		query := fmt.Sprintf(`DELETE FROM %s WHERE %s = ? AND %s = ?`,
			consts.DBBlockedDomains,
			consts.QBlockedDomain,
			consts.QBlockedContext,
		)

		if _, err := db.Exec(query, normalizedDomain, string(context)); err != nil {
			return fmt.Errorf("failed to unblock domain %q (context: %s) from database: %w", normalizedDomain, context, err)
		}

		logger.Pl.S("Unblocked domain %q for context %q", normalizedDomain, context)
	}

	return nil
}

// LoadBlockedDomains loads all blocked domains from the database into memory on startup.
func LoadBlockedDomains(db *sql.DB) error {
	query := fmt.Sprintf(`SELECT %s, %s, %s FROM %s`,
		consts.QBlockedDomain,
		consts.QBlockedContext,
		consts.QBlockedAt,
		consts.DBBlockedDomains,
	)

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to load blocked domains: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Pl.E("Could not close rows for blocked domains: %v", closeErr)
		}
	}()

	vars.BlockedDomainsMutex.Lock()
	defer vars.BlockedDomainsMutex.Unlock()

	if vars.BlockedDomains == nil {
		vars.BlockedDomains = make(map[string]map[vars.BlockContext]time.Time)
	}

	count := 0
	for rows.Next() {
		var domain, contextStr string
		var blockedAt time.Time

		if err := rows.Scan(&domain, &contextStr, &blockedAt); err != nil {
			logger.Pl.W("Failed to scan blocked domain row: %v", err)
			continue
		}

		context := vars.BlockContext(contextStr)
		if vars.BlockedDomains[domain] == nil {
			vars.BlockedDomains[domain] = make(map[vars.BlockContext]time.Time)
		}
		vars.BlockedDomains[domain][context] = blockedAt
		count++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating blocked domains: %w", err)
	}

	if count > 0 {
		logger.Pl.I("Loaded %d blocked domain(s) from database", count)
	}

	return nil
}

// CleanExpiredBlocks removes expired blocks from memory and database.
//
// Should be called periodically (e.g., on startup or during crawl watchdog).
func CleanExpiredBlocks(db *sql.DB) error {
	vars.BlockedDomainsMutex.Lock()
	defer vars.BlockedDomainsMutex.Unlock()

	domainsToRemove := make([]struct {
		domain  string
		context vars.BlockContext
	}, 0)

	// Find expired blocks
	for domain, contexts := range vars.BlockedDomains {
		for context, blockedTime := range contexts {
			timeoutMinutes := GetTimeoutForDomain(domain)
			timeout := time.Duration(timeoutMinutes) * time.Minute
			unlockTime := blockedTime.Add(timeout)

			if time.Now().After(unlockTime) {
				domainsToRemove = append(domainsToRemove, struct {
					domain  string
					context vars.BlockContext
				}{domain, context})
			}
		}
	}

	// Remove expired blocks.
	for _, item := range domainsToRemove {
		// Remove from memory.
		if vars.BlockedDomains[item.domain] != nil {
			delete(vars.BlockedDomains[item.domain], item.context)
			if len(vars.BlockedDomains[item.domain]) == 0 {
				delete(vars.BlockedDomains, item.domain)
			}
		}

		// Remove from database.
		query := fmt.Sprintf(`DELETE FROM %s WHERE %s = ? AND %s = ?`,
			consts.DBBlockedDomains,
			consts.QBlockedDomain,
			consts.QBlockedContext,
		)

		if _, err := db.Exec(query, item.domain, string(item.context)); err != nil {
			logger.Pl.E("Failed to remove expired block for domain %q (context: %s): %v", item.domain, item.context, err)
		} else {
			logger.Pl.S("Removed expired block for domain %q (context: %s)", item.domain, item.context)
		}
	}

	return nil
}

// GetTimeoutForDomain returns the timeout in minutes for a given domain.
//
// Uses BotTimeoutMap if domain matches, otherwise returns default of 12 hours (720 minutes).
func GetTimeoutForDomain(domain string) float64 {
	for key, timeout := range consts.BotTimeoutMap {
		if strings.Contains(domain, key) {
			return timeout
		}
	}
	return 720.0 // Default: 12 hours.
}

// GetAllBlockedDomains returns all currently blocked domains with their contexts and block times.
func GetAllBlockedDomains() map[string]map[vars.BlockContext]time.Time {
	vars.BlockedDomainsMutex.RLock()
	defer vars.BlockedDomainsMutex.RUnlock()

	// Create a deep copy to avoid race conditions.
	result := make(map[string]map[vars.BlockContext]time.Time)
	for domain, contexts := range vars.BlockedDomains {
		result[domain] = make(map[vars.BlockContext]time.Time)
		maps.Copy(result[domain], contexts)
	}

	return result
}

// normalizeDomain extracts the eTLD+1 (effective top-level domain + 1 label).
//
// e.g., m.google.com -> google.com, www.bbc.co.uk -> bbc.co.uk.
func normalizeDomain(rawDomain string) string {
	if domain, err := publicsuffix.EffectiveTLDPlusOne(rawDomain); err == nil {
		return strings.ToLower(domain)
	}
	return strings.ToLower(rawDomain)
}
