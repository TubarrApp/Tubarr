package models

import (
	"time"
)

// Profile is a named set of settings, filling in those a channel or channel URL using it leaves unset.
type Profile struct {
	ID                int64       `db:"id"`
	Name              string      `db:"name"`
	ProfileSettings   *Settings   `db:"settings"`
	ProfileMetarrArgs *MetarrArgs `db:"metarr"`
	CreatedAt         time.Time   `db:"created_at"`
	UpdatedAt         time.Time   `db:"updated_at"`
	LoadErr           error       // Why its saved settings failed validation on load: it can be listed, fixed or deleted, but not used.
}
