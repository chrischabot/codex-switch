package store

import (
	"errors"
	"fmt"
	"strings"
)

// RecordCompletedResponse preserves directly observed inference usage once per response.
func (database *StoreDatabase) RecordCompletedResponse(record CompletedResponseRecord) error {
	if strings.TrimSpace(string(record.CompletedResponseID)) == "" {
		return errors.New("completed response is missing provider identity")
	}
	if record.InputTokens < 0 || record.CachedInputTokens < 0 || record.OutputTokens < 0 || record.ReasoningTokens < 0 ||
		record.CachedInputTokens > record.InputTokens || record.ReasoningTokens > record.OutputTokens {
		return errors.New("completed response has invalid token usage")
	}
	if record.CacheWriteInputTokens != nil && (*record.CacheWriteInputTokens < 0 ||
		*record.CacheWriteInputTokens > record.InputTokens-record.CachedInputTokens) {
		return errors.New("completed response has invalid cache-write token usage")
	}
	_, err := database.sqlDatabase.Exec(`
INSERT INTO completed_responses (
    completed_response_id, codex_task_id, account_name, model_slug, harness_mode,
    input_tokens, cached_input_tokens, cache_write_input_tokens, output_tokens, reasoning_tokens,
    completed_at_unix_nano
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.CompletedResponseID, record.CodexTaskID, record.AccountName, record.ModelSlug, record.HarnessMode,
		record.InputTokens, record.CachedInputTokens, record.CacheWriteInputTokens, record.OutputTokens, record.ReasoningTokens,
		record.CompletedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("record completed response: %w", err)
	}
	return nil
}
