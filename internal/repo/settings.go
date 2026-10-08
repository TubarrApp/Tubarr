package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
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

// GetDomainLimits returns the domain download limits map from the settings table.
func (ss *TubarrSettingsStore) GetDomainLimits() (map[string]int, error) {
	val, found, err := ss.GetSetting(keys.DomainDownloadLimits)
	if err != nil {
		return nil, err
	}
	limits := make(map[string]int)
	if !found || val == "" || val == "{}" {
		return limits, nil
	}
	if err := json.Unmarshal([]byte(val), &limits); err != nil {
		return nil, fmt.Errorf("failed to parse domain limits JSON: %w", err)
	}
	return limits, nil
}

// SetDomainLimit sets the download concurrency limit for a specific hostname.
// A concurrency of 0 removes the limit for that hostname.
func (ss *TubarrSettingsStore) SetDomainLimit(hostname string, concurrency int) error {
	limits, err := ss.GetDomainLimits()
	if err != nil {
		return err
	}
	if concurrency <= 0 {
		delete(limits, hostname)
	} else {
		limits[hostname] = concurrency
	}
	data, err := json.Marshal(limits)
	if err != nil {
		return fmt.Errorf("failed to marshal domain limits: %w", err)
	}
	logger.Pl.I("Domain download limits updated: %s", string(data))
	return ss.SetSetting(keys.DomainDownloadLimits, string(data))
}

// DeleteDomainLimit removes the download concurrency limit for a specific hostname.
func (ss *TubarrSettingsStore) DeleteDomainLimit(hostname string) error {
	return ss.SetDomainLimit(hostname, 0)
}
