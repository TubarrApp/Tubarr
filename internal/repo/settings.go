package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// TubarrSettingsStore holds a pointer to the sql.DB.
type TubarrSettingsStore struct {
	DB *sql.DB
}

// GetTubarrSettingsStore returns a settings store instance with injected database.
func GetTubarrSettingsStore(db *sql.DB) *TubarrSettingsStore {
	return &TubarrSettingsStore{DB: db}
}

// GetDB returns the database.
func (ss *TubarrSettingsStore) GetDB() *sql.DB {
	return ss.DB
}

// GetSetting retrieves a setting value by key.
func (ss *TubarrSettingsStore) GetSetting(key string) (string, bool, error) {
	var val string
	err := ss.DB.QueryRow(
		fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", consts.QSettingsVal, consts.DBSettings, consts.QSettingsKey),
		key,
	).Scan(&val)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to get setting %q: %w", key, err)
	}
	return val, true, nil
}

// SetSetting upserts a setting value by key.
func (ss *TubarrSettingsStore) SetSetting(key, value string) error {
	_, err := ss.DB.Exec(
		fmt.Sprintf("INSERT OR REPLACE INTO %s (%s, %s) VALUES (?, ?)", consts.DBSettings, consts.QSettingsKey, consts.QSettingsVal),
		key, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set setting %q: %w", key, err)
	}
	return nil
}

// GetDomainLimits returns the per-domain download and crawl limits.
func (ss *TubarrSettingsStore) GetDomainLimits() (map[string]models.DomainLimit, error) {
	downloads, err := ss.GetDomainLimitMap(keys.DomainDownloadLimits)
	if err != nil {
		return nil, err
	}
	crawls, err := ss.GetDomainLimitMap(keys.DomainCrawlLimits)
	if err != nil {
		return nil, err
	}
	limits := make(map[string]models.DomainLimit, len(downloads))
	for hostname, n := range downloads {
		limits[hostname] = models.DomainLimit{Download: n}
	}
	for hostname, n := range crawls {
		l := limits[hostname]
		l.Crawl = n
		limits[hostname] = l
	}
	return limits, nil
}

// GetDomainLimitMap returns the hostname to limit map stored under a domain limits setting key.
func (ss *TubarrSettingsStore) GetDomainLimitMap(key string) (map[string]int, error) {
	val, found, err := ss.GetSetting(key)
	if err != nil {
		return nil, err
	}
	limits := make(map[string]int)
	if !found || val == "" || val == "{}" {
		return limits, nil
	}
	if err := json.Unmarshal([]byte(val), &limits); err != nil {
		return nil, fmt.Errorf("failed to parse %s JSON: %w", key, err)
	}
	return limits, nil
}

// SetDomainLimit sets the download and crawl limits for a hostname. A limit of 0 removes it.
func (ss *TubarrSettingsStore) SetDomainLimit(hostname string, limit models.DomainLimit) error {
	if err := ss.setDomainLimitValue(keys.DomainDownloadLimits, hostname, limit.Download); err != nil {
		return err
	}
	return ss.setDomainLimitValue(keys.DomainCrawlLimits, hostname, limit.Crawl)
}

// DeleteDomainLimit removes the download and crawl limits for a hostname.
func (ss *TubarrSettingsStore) DeleteDomainLimit(hostname string) error {
	return ss.SetDomainLimit(hostname, models.DomainLimit{})
}

// setDomainLimitValue sets one hostname's limit under a domain limits setting key.
func (ss *TubarrSettingsStore) setDomainLimitValue(key, hostname string, limit int) error {
	limits, err := ss.GetDomainLimitMap(key)
	if err != nil {
		return err
	}
	if limit <= 0 {
		delete(limits, hostname)
	} else {
		limits[hostname] = limit
	}
	data, err := json.Marshal(limits)
	if err != nil {
		return fmt.Errorf("failed to marshal %s: %w", key, err)
	}
	logger.Pl.I("Setting %s updated: %s", key, string(data))
	return ss.SetSetting(key, string(data))
}
