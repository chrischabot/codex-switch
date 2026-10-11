package usage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/josh/codex-switch/internal/accounts"
)

func createOpenAIDailyTokenHistorySchema(database *sql.DB) error {
	_, err := database.Exec(`
CREATE TABLE IF NOT EXISTS provider_token_histories (
    provider_account_id TEXT PRIMARY KEY,
    read_started_at_unix_nano INTEGER NOT NULL,
    reporting_date TEXT NOT NULL,
    generated_at_unix_nano INTEGER NOT NULL,
    lifetime_tokens INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS provider_daily_token_buckets (
    provider_account_id TEXT NOT NULL,
    start_date TEXT NOT NULL,
    tokens INTEGER NOT NULL,
    PRIMARY KEY (provider_account_id, start_date)
) STRICT;

CREATE TABLE IF NOT EXISTS provider_daily_allowance_shares (
    provider_account_id TEXT NOT NULL,
    date TEXT NOT NULL,
    model TEXT NOT NULL,
    fast INTEGER NOT NULL,
    percent REAL NOT NULL,
    PRIMARY KEY (provider_account_id, date, model, fast)
) STRICT;`)
	return err
}

// saveOpenAIDailyTokenHistory replaces one validated provider report atomically.
// A slower read from a draining proxy cannot replace a newer provider report.
func (ledger *UsageLedger) saveOpenAIDailyTokenHistory(history accounts.OpenAIDailyTokenHistory) (bool, error) {
	transaction, err := ledger.sqlDatabase.Begin()
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

// readOpenAIDailyTokenHistories reads report metadata and buckets in one SQLite snapshot.
func (ledger *UsageLedger) readOpenAIDailyTokenHistories() (map[string]accounts.OpenAIDailyTokenHistory, error) {
	rows, err := ledger.sqlDatabase.Query(`
SELECT h.provider_account_id, h.read_started_at_unix_nano, h.reporting_date,
       h.generated_at_unix_nano, h.lifetime_tokens, b.start_date, b.tokens
FROM provider_token_histories h
LEFT JOIN provider_daily_token_buckets b USING (provider_account_id)
ORDER BY h.provider_account_id, b.start_date`)
	if err != nil {
		return nil, fmt.Errorf("read provider token histories: %w", err)
	}
	defer rows.Close()
	histories := make(map[string]accounts.OpenAIDailyTokenHistory)
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
			history = accounts.OpenAIDailyTokenHistory{
				AccountID: accountID, ReadStartedAt: time.Unix(0, readStartedAt).UTC(),
				ReportingDate: date, GeneratedAt: time.Unix(0, generatedAt).UTC(), LifetimeTokens: lifetimeTokens,
			}
		}
		if bucketDate.Valid {
			history.Buckets = append(history.Buckets, accounts.OpenAIDailyTokenBucket{StartDate: bucketDate.String, Tokens: bucketTokens.Int64})
		}
		histories[accountID] = history
	}
	return histories, rows.Err()
}

// saveOpenAIAllowanceShares replaces one account's shares from firstDate on.
func (ledger *UsageLedger) saveOpenAIAllowanceShares(accountID string, firstDate time.Time, shares []accounts.OpenAIAllowanceShare) error {
	transaction, err := ledger.sqlDatabase.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec(`DELETE FROM provider_daily_allowance_shares WHERE provider_account_id = ? AND date >= ?`, accountID, firstDate.Format(time.DateOnly)); err != nil {
		return err
	}
	for _, share := range shares {
		if _, err := transaction.Exec(`INSERT INTO provider_daily_allowance_shares (provider_account_id, date, model, fast, percent) VALUES (?, ?, ?, ?, ?)`, accountID, share.Date, share.Model, share.Fast, share.Percent); err != nil {
			return fmt.Errorf("record OpenAI allowance share: %w", err)
		}
	}
	return transaction.Commit()
}

// readOpenAIAllowanceShares returns every stored share by provider account ID.
func (ledger *UsageLedger) readOpenAIAllowanceShares() (map[string][]accounts.OpenAIAllowanceShare, error) {
	rows, err := ledger.sqlDatabase.Query(`SELECT provider_account_id, date, model, fast, percent FROM provider_daily_allowance_shares ORDER BY provider_account_id, date, model, fast`)
	if err != nil {
		return nil, fmt.Errorf("read OpenAI allowance shares: %w", err)
	}
	defer rows.Close()
	shares := make(map[string][]accounts.OpenAIAllowanceShare)
	for rows.Next() {
		var accountID string
		var share accounts.OpenAIAllowanceShare
		if err := rows.Scan(&accountID, &share.Date, &share.Model, &share.Fast, &share.Percent); err != nil {
			return nil, err
		}
		shares[accountID] = append(shares[accountID], share)
	}
	return shares, rows.Err()
}
