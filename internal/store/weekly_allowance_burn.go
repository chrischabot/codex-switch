package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
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

type weeklyAllowanceBurn struct {
	accountNames        []AccountName
	computedAt          time.Time
	expiresAt           time.Time
	earliestObservation time.Time
	usedPercent         float64
}

func (burn weeklyAllowanceBurn) selects(accountNames []AccountName) bool {
	for _, name := range accountNames {
		if !slices.Contains(burn.accountNames, name) {
			return false
		}
	}
	for _, name := range burn.accountNames {
		if !slices.Contains(accountNames, name) {
			return false
		}
	}
	return true
}

func (burn weeklyAllowanceBurn) value(now time.Time) *float64 {
	if burn.earliestObservation.IsZero() || now.Sub(burn.earliestObservation) < 24*time.Hour {
		return nil
	}
	coveredFor := min(now.Sub(burn.earliestObservation), allowanceWeek)
	accounts := burn.usedPercent / 100 * float64(allowanceWeek) / float64(coveredFor)
	return &accounts
}

// ReadWeeklyAllowanceBurn counts increases at their observation time. The first
// reading of an account is a baseline; the first reading after a reset is usage.
// Until a reading is added or ages out, only the observed coverage changes.
func (database *StoreDatabase) ReadWeeklyAllowanceBurn(accountNames []AccountName, now time.Time) (*float64, error) {
	database.weeklyAllowanceBurnMutex.Lock()
	defer database.weeklyAllowanceBurnMutex.Unlock()
	var latestRowID sql.NullInt64
	if err := database.sqlDatabase.QueryRow(`SELECT MAX(rowid) FROM weekly_allowance_readings`).Scan(&latestRowID); err != nil {
		return nil, fmt.Errorf("read weekly allowance changes: %w", err)
	}
	if latestRowID.Int64 != database.weeklyAllowanceBurnRowID {
		database.weeklyAllowanceBurns = nil
		database.weeklyAllowanceBurnRowID = latestRowID.Int64
	}
	cachedIndex := -1
	for index, burn := range database.weeklyAllowanceBurns {
		if !burn.selects(accountNames) {
			continue
		}
		if !now.Before(burn.computedAt) && (burn.expiresAt.IsZero() || now.Before(burn.expiresAt)) {
			if index == 0 && len(database.weeklyAllowanceBurns) == 2 {
				database.weeklyAllowanceBurns[0], database.weeklyAllowanceBurns[1] = database.weeklyAllowanceBurns[1], burn
			}
			return burn.value(now), nil
		}
		cachedIndex = index
		break
	}
	burn, err := database.readWeeklyAllowanceBurn(accountNames, now)
	if err != nil {
		return nil, err
	}
	if cachedIndex >= 0 {
		database.weeklyAllowanceBurns[cachedIndex] = burn
	} else if len(database.weeklyAllowanceBurns) == 2 {
		// The menu reads ChatGPT and Claude separately. Keep their two most
		// recent selections without retaining historical pool combinations.
		database.weeklyAllowanceBurns[0] = database.weeklyAllowanceBurns[1]
		database.weeklyAllowanceBurns[1] = burn
	} else {
		database.weeklyAllowanceBurns = append(database.weeklyAllowanceBurns, burn)
	}
	return burn.value(now), nil
}

func (database *StoreDatabase) readWeeklyAllowanceBurn(accountNames []AccountName, now time.Time) (weeklyAllowanceBurn, error) {
	burn := weeklyAllowanceBurn{accountNames: slices.Clone(accountNames), computedAt: now}
	selectedAccounts := make(map[AccountName]bool, len(accountNames))
	for _, accountName := range accountNames {
		selectedAccounts[accountName] = true
	}
	rows, err := database.sqlDatabase.Query(`
SELECT account_name, resets_at_unix, used_percent, observed_at_unix_nano
FROM weekly_allowance_readings
WHERE observed_at_unix_nano >= ?
ORDER BY account_name, observed_at_unix_nano`, now.Add(-8*24*time.Hour).UnixNano())
	if err != nil {
		return burn, fmt.Errorf("read weekly allowance burn: %w", err)
	}
	defer rows.Close()
	expiresAt := func(at time.Time) {
		if burn.expiresAt.IsZero() || at.Before(burn.expiresAt) {
			burn.expiresAt = at
		}
	}
	var previous WeeklyAllowanceReading
	// windowHigh is the highest reading of the account's current window. Two
	// sources report the same window with different rounding and lag (Claude's
	// usage API and its response headers), so readings within a window go up
	// and down; only usage above the window's earlier high is new.
	var windowHigh float64
	for rows.Next() {
		var reading WeeklyAllowanceReading
		var resetsAt, observedAt int64
		if err := rows.Scan(&reading.AccountName, &resetsAt, &reading.UsedPercent, &observedAt); err != nil {
			return burn, err
		}
		if !selectedAccounts[reading.AccountName] {
			continue
		}
		reading.ResetsAt, reading.ObservedAt = time.Unix(resetsAt, 0), time.Unix(0, observedAt)
		if reading.ObservedAt.After(now) {
			expiresAt(reading.ObservedAt)
			continue
		}
		expiresAt(reading.ObservedAt.Add(8*24*time.Hour + time.Nanosecond))
		if burn.earliestObservation.IsZero() || reading.ObservedAt.Before(burn.earliestObservation) {
			burn.earliestObservation = reading.ObservedAt
		}
		if previous.AccountName == reading.AccountName && !reading.ObservedAt.Before(now.Add(-allowanceWeek)) {
			expiresAt(reading.ObservedAt.Add(allowanceWeek + time.Nanosecond))
			increase := reading.UsedPercent
			// Providers report the same reset a second or a minute apart; only a
			// reset that moves by more than that starts a new window.
			if sameAllowanceWindow(previous, reading) {
				increase -= windowHigh
			}
			burn.usedPercent += math.Max(0, increase)
		}
		if previous.AccountName == reading.AccountName && sameAllowanceWindow(previous, reading) {
			windowHigh = math.Max(windowHigh, reading.UsedPercent)
		} else {
			windowHigh = reading.UsedPercent
		}
		previous = reading
	}
	return burn, rows.Err()
}

// sameAllowanceWindow reports whether two readings of an account belong to
// the same allowance window.
func sameAllowanceWindow(previous, reading WeeklyAllowanceReading) bool {
	return reading.ResetsAt.Sub(previous.ResetsAt).Abs() < time.Hour
}
