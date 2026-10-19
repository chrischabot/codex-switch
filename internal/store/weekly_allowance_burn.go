package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

const allowanceWeek = 7 * 24 * time.Hour

type WeeklyAllowanceReading struct {
	AccountName AccountName
	ResetsAt    time.Time
	UsedPercent float64
	ObservedAt  time.Time
}

func createWeeklyAllowanceSchema(database *sql.DB) error {
	_, err := database.Exec(`
CREATE TABLE IF NOT EXISTS weekly_allowance_readings (
    account_name TEXT NOT NULL,
    resets_at_unix INTEGER NOT NULL,
    used_percent REAL NOT NULL,
    observed_at_unix_nano INTEGER NOT NULL,
    PRIMARY KEY (account_name, observed_at_unix_nano)
) STRICT;`)
	return err
}

func (database *StoreDatabase) RecordWeeklyAllowanceReading(reading WeeklyAllowanceReading) error {
	if reading.AccountName == "" || reading.ResetsAt.IsZero() || reading.ObservedAt.IsZero() ||
		math.IsNaN(reading.UsedPercent) || math.IsInf(reading.UsedPercent, 0) || reading.UsedPercent < 0 {
		return errors.New("weekly allowance reading requires an account, reset, observation time, and nonnegative usage")
	}
	if _, err := database.sqlDatabase.Exec(`DELETE FROM weekly_allowance_readings WHERE observed_at_unix_nano < ?`,
		reading.ObservedAt.Add(-8*24*time.Hour).UnixNano()); err != nil {
		return fmt.Errorf("prune weekly allowance readings: %w", err)
	}
	_, err := database.sqlDatabase.Exec(`
WITH latest AS (
    SELECT resets_at_unix, used_percent, observed_at_unix_nano
    FROM weekly_allowance_readings WHERE account_name = ?
    ORDER BY observed_at_unix_nano DESC LIMIT 1
)
INSERT INTO weekly_allowance_readings
    (account_name, resets_at_unix, used_percent, observed_at_unix_nano)
SELECT ?, ?, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM latest)
   OR EXISTS (SELECT 1 FROM latest WHERE observed_at_unix_nano < ?
              AND (resets_at_unix != ? OR used_percent != ?))`,
		reading.AccountName, reading.AccountName, reading.ResetsAt.Unix(), reading.UsedPercent,
		reading.ObservedAt.UnixNano(), reading.ObservedAt.UnixNano(), reading.ResetsAt.Unix(), reading.UsedPercent)
	if err != nil {
		return fmt.Errorf("record weekly allowance reading: %w", err)
	}
	return nil
}

// ReadWeeklyAllowanceBurn counts increases at their observation time. The first
// reading of an account is a baseline; the first reading after a reset is usage.
func (database *StoreDatabase) ReadWeeklyAllowanceBurn(accountNames []AccountName, now time.Time) (*float64, error) {
	selectedAccounts := make(map[AccountName]bool, len(accountNames))
	for _, accountName := range accountNames {
		selectedAccounts[accountName] = true
	}
	rows, err := database.sqlDatabase.Query(`
SELECT account_name, resets_at_unix, used_percent, observed_at_unix_nano
FROM weekly_allowance_readings
WHERE observed_at_unix_nano >= ? AND observed_at_unix_nano <= ?
ORDER BY account_name, observed_at_unix_nano`, now.Add(-8*24*time.Hour).UnixNano(), now.UnixNano())
	if err != nil {
		return nil, fmt.Errorf("read weekly allowance burn: %w", err)
	}
	defer rows.Close()
	var earliestObservation time.Time
	var previous WeeklyAllowanceReading
	var usedPercent float64
	for rows.Next() {
		var reading WeeklyAllowanceReading
		var resetsAt, observedAt int64
		if err := rows.Scan(&reading.AccountName, &resetsAt, &reading.UsedPercent, &observedAt); err != nil {
			return nil, err
		}
		if !selectedAccounts[reading.AccountName] {
			continue
		}
		reading.ResetsAt, reading.ObservedAt = time.Unix(resetsAt, 0), time.Unix(0, observedAt)
		if earliestObservation.IsZero() || reading.ObservedAt.Before(earliestObservation) {
			earliestObservation = reading.ObservedAt
		}
		if previous.AccountName == reading.AccountName && !reading.ObservedAt.Before(now.Add(-allowanceWeek)) {
			increase := reading.UsedPercent
			// Providers report the same reset a second or a minute apart; only a
			// reset that moves by more than that starts a new window.
			if reading.ResetsAt.Sub(previous.ResetsAt).Abs() < time.Hour {
				increase -= previous.UsedPercent
			}
			usedPercent += math.Max(0, increase)
		}
		previous = reading
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if earliestObservation.IsZero() || now.Sub(earliestObservation) < 24*time.Hour {
		return nil, nil
	}
	coveredFor := min(now.Sub(earliestObservation), allowanceWeek)
	accounts := usedPercent / 100 * float64(allowanceWeek) / float64(coveredFor)
	return &accounts, nil
}
