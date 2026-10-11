package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ProviderAccountID string

// AccountAllowanceWindowIdentity records an explicit provider reset from an
// authenticated allowance read after the account has used that window.
type AccountAllowanceWindowIdentity struct {
	ProviderAccountID ProviderAccountID
	DurationSeconds   int64
	ResetsAt          time.Time
	ObservedAt        time.Time
	UsedPercent       float64
}

func createAllowanceWindowIdentitySchema(database *sql.DB) error {
	_, err := database.Exec(`
CREATE TABLE IF NOT EXISTS account_allowance_window_identities (
    provider_account_id TEXT NOT NULL,
    duration_seconds INTEGER NOT NULL,
    resets_at_unix INTEGER NOT NULL,
    observed_at_unix_nano INTEGER NOT NULL,
    used_percent REAL NOT NULL,
    PRIMARY KEY (duration_seconds, resets_at_unix, provider_account_id)
) STRICT;`)
	return err
}

func (database *StoreDatabase) RecordAccountAllowanceWindowIdentity(identity AccountAllowanceWindowIdentity) error {
	if identity.ProviderAccountID == "" || identity.DurationSeconds <= 0 ||
		identity.ResetsAt.IsZero() || identity.ObservedAt.IsZero() ||
		!(identity.UsedPercent > 0 && identity.UsedPercent <= 100) {
		return errors.New("allowance identity requires an account, a window, and positive observed usage")
	}
	_, err := database.sqlDatabase.Exec(`
INSERT INTO account_allowance_window_identities
    (provider_account_id, duration_seconds, resets_at_unix, observed_at_unix_nano, used_percent)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(duration_seconds, resets_at_unix, provider_account_id) DO NOTHING`,
		identity.ProviderAccountID, identity.DurationSeconds, identity.ResetsAt.Unix(),
		identity.ObservedAt.UnixNano(), identity.UsedPercent)
	if err != nil {
		return fmt.Errorf("record allowance window identity: %w", err)
	}
	return nil
}

// FindAccountForAllowanceWindow identifies only events within a window that has
// exactly one authenticated owner. Colliding resets do not establish ownership.
func (database *StoreDatabase) FindAccountForAllowanceWindow(durationSeconds int64, resetsAt, eventAt time.Time) (ProviderAccountID, bool, error) {
	if durationSeconds <= 0 || resetsAt.IsZero() || eventAt.IsZero() ||
		eventAt.Unix() < resetsAt.Unix()-durationSeconds || !eventAt.Before(resetsAt) {
		return "", false, nil
	}
	var owner ProviderAccountID
	err := database.sqlDatabase.QueryRow(`
SELECT MIN(provider_account_id)
FROM account_allowance_window_identities
WHERE duration_seconds = ? AND resets_at_unix = ?
HAVING COUNT(DISTINCT provider_account_id) = 1`, durationSeconds, resetsAt.Unix()).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find allowance window owner: %w", err)
	}
	return owner, true, nil
}
