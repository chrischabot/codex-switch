package store

import (
	"database/sql"
	"fmt"
	"time"
)

// RecordProviderTokenHistory replaces one validated provider report atomically.
// A slower read from a draining proxy cannot replace a newer provider report.
func (database *StoreDatabase) RecordProviderTokenHistory(history OpenAIDailyTokenHistory) (bool, error) {
	transaction, err := database.sqlDatabase.Begin()
	if err != nil {
		return false, err
	}
	defer transaction.Rollback()
	result, err := transaction.Exec(`
INSERT INTO provider_token_histories
    (provider_account_id, read_started_at_unix_nano, reporting_date, generated_at_unix_nano, lifetime_tokens)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(provider_account_id) DO UPDATE SET
    read_started_at_unix_nano = excluded.read_started_at_unix_nano,
    reporting_date = excluded.reporting_date,
    generated_at_unix_nano = excluded.generated_at_unix_nano,
    lifetime_tokens = excluded.lifetime_tokens
WHERE excluded.generated_at_unix_nano > provider_token_histories.generated_at_unix_nano
   OR (excluded.generated_at_unix_nano = provider_token_histories.generated_at_unix_nano
       AND excluded.read_started_at_unix_nano >= provider_token_histories.read_started_at_unix_nano)`,
		history.AccountID, history.ReadStartedAt.UnixNano(), history.ReportingDate.Format("2006-01-02"), history.GeneratedAt.UnixNano(), history.LifetimeTokens)
	if err != nil {
		return false, fmt.Errorf("record provider token history: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil || updated == 0 {
		return false, err
	}
	if _, err := transaction.Exec(`DELETE FROM provider_daily_token_buckets WHERE provider_account_id = ?`, history.AccountID); err != nil {
		return false, err
	}
	for _, bucket := range history.Buckets {
		if _, err := transaction.Exec(`INSERT INTO provider_daily_token_buckets (provider_account_id, start_date, tokens) VALUES (?, ?, ?)`, history.AccountID, bucket.StartDate, bucket.Tokens); err != nil {
			return false, fmt.Errorf("record provider daily token bucket: %w", err)
		}
	}
	return true, transaction.Commit()
}

// ReadProviderTokenHistories reads report metadata and buckets in one SQLite snapshot.
func (database *StoreDatabase) ReadProviderTokenHistories() (map[string]OpenAIDailyTokenHistory, error) {
	rows, err := database.sqlDatabase.Query(`
SELECT h.provider_account_id, h.read_started_at_unix_nano, h.reporting_date,
       h.generated_at_unix_nano, h.lifetime_tokens, b.start_date, b.tokens
FROM provider_token_histories h
LEFT JOIN provider_daily_token_buckets b USING (provider_account_id)
ORDER BY h.provider_account_id, b.start_date`)
	if err != nil {
		return nil, fmt.Errorf("read provider token histories: %w", err)
	}
	defer rows.Close()
	histories := make(map[string]OpenAIDailyTokenHistory)
	for rows.Next() {
		var accountID, reportingDate string
		var readStartedAt, generatedAt, lifetimeTokens int64
		var bucketDate sql.NullString
		var bucketTokens sql.NullInt64
		if err := rows.Scan(&accountID, &readStartedAt, &reportingDate, &generatedAt, &lifetimeTokens, &bucketDate, &bucketTokens); err != nil {
			return nil, err
		}
		history, exists := histories[accountID]
		if !exists {
			date, err := time.Parse("2006-01-02", reportingDate)
			if err != nil {
				return nil, fmt.Errorf("read provider reporting date: %w", err)
			}
			history = OpenAIDailyTokenHistory{
				AccountID: accountID, ReadStartedAt: time.Unix(0, readStartedAt).UTC(),
				ReportingDate: date, GeneratedAt: time.Unix(0, generatedAt).UTC(), LifetimeTokens: lifetimeTokens,
			}
		}
		if bucketDate.Valid {
			history.Buckets = append(history.Buckets, OpenAIDailyTokenBucket{StartDate: bucketDate.String, Tokens: bucketTokens.Int64})
		}
		histories[accountID] = history
	}
	return histories, rows.Err()
}
