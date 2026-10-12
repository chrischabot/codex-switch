package store

import "time"

func (database *StoreDatabase) ReadCompletedResponsesSince(since time.Time) ([]CompletedResponseRecord, error) {
	rows, err := database.sqlDatabase.Query(`SELECT completed_response_id,codex_task_id,account_name,model_slug,harness_mode,input_tokens,cached_input_tokens,cache_write_input_tokens,output_tokens,reasoning_tokens,completed_at_unix_nano,cost_nano_usd FROM completed_responses WHERE completed_at_unix_nano >= ? ORDER BY completed_at_unix_nano DESC`, since.UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []CompletedResponseRecord
	for rows.Next() {
		var record CompletedResponseRecord
		var completedAt int64
		if err := rows.Scan(&record.CompletedResponseID, &record.TaskID, &record.AccountName, &record.ModelSlug, &record.HarnessMode, &record.InputTokens, &record.CachedInputTokens, &record.CacheWriteInputTokens, &record.OutputTokens, &record.ReasoningTokens, &completedAt, &record.CostNanoUSD); err != nil {
			return nil, err
		}
		record.CompletedAt = time.Unix(0, completedAt)
		records = append(records, record)
	}
	return records, rows.Err()
}
