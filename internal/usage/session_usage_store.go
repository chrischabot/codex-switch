package usage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func createSessionUsageSchema(database *sql.DB) error {
	_, err := database.Exec(`
CREATE TABLE IF NOT EXISTS native_usage_sources (
 task_id TEXT PRIMARY KEY, path TEXT NOT NULL, identity TEXT NOT NULL, size INTEGER NOT NULL, modified INTEGER NOT NULL,
 offset INTEGER NOT NULL, model TEXT NOT NULL, tier TEXT, turn_id TEXT NOT NULL, history_ended INTEGER NOT NULL, canonical_seen INTEGER NOT NULL,
 previous_input INTEGER, previous_cached INTEGER, previous_cache_write INTEGER, previous_output INTEGER, previous_reasoning INTEGER, previous_total INTEGER,
 parent_task_id TEXT NOT NULL, forked_from_task_id TEXT NOT NULL, created INTEGER NOT NULL, source_kind TEXT NOT NULL, pending_response_id TEXT NOT NULL, pending_input INTEGER, pending_cached INTEGER, pending_cache_write INTEGER, pending_output INTEGER, pending_reasoning INTEGER, pending_total INTEGER
) STRICT;
CREATE TABLE IF NOT EXISTS native_usage_observations (
 task_id TEXT NOT NULL, source_offset INTEGER NOT NULL, response_id TEXT NOT NULL, root_turn_id TEXT NOT NULL, session_id TEXT NOT NULL,
 turn_id TEXT NOT NULL, observed INTEGER NOT NULL, model TEXT NOT NULL, tier TEXT,
 input INTEGER, cached INTEGER, cache_write INTEGER, output INTEGER, reasoning INTEGER, total INTEGER,
 limit_id TEXT, plan_type TEXT, primary_used REAL, primary_minutes INTEGER, primary_resets INTEGER,
 secondary_used REAL, secondary_minutes INTEGER, secondary_resets INTEGER,
 PRIMARY KEY(task_id,source_offset)
) STRICT;
CREATE INDEX IF NOT EXISTS native_usage_observed ON native_usage_observations(observed);
CREATE TABLE IF NOT EXISTS native_task_child_records (
 source_task_id TEXT NOT NULL, source_offset INTEGER NOT NULL, child_task_id TEXT NOT NULL,
 PRIMARY KEY(source_task_id,source_offset)
) STRICT;
`)
	return err
}

func (ledger *UsageLedger) importCodexSessions(ctx context.Context, home string, since time.Time) (sessionImportSummary, error) {
	sources, err := discoverCodexSessions(home, since)
	if err != nil {
		return sessionImportSummary{}, err
	}
	return ledger.importSessionFiles(ctx, sources, readCodexSession)
}

func (ledger *UsageLedger) importPiSessions(ctx context.Context, sessionDirectory string, since time.Time) (sessionImportSummary, error) {
	sources, err := discoverPiSessions(sessionDirectory, since)
	if err != nil {
		return sessionImportSummary{}, err
	}
	return ledger.importSessionFiles(ctx, sources, readPiSession)
}

func (ledger *UsageLedger) importSessionFiles(ctx context.Context, sources []sessionFile, read func(context.Context, sessionFile, sessionReadCursor) (sessionReadResult, error)) (sessionImportSummary, error) {
	var result sessionImportSummary
	var err error
	result.FilesDiscovered = len(sources)
	for _, source := range sources {
		var cursor sessionReadCursor
		var previousIdentity, previousPath, previousParentTaskID string
		var previousSize, modified int64
		var input, cached, cacheWrite, output, reasoning, total sql.NullInt64
		var pendingInput, pendingCached, pendingCacheWrite, pendingOutput, pendingReasoning, pendingTotal sql.NullInt64
		err = ledger.sqlDatabase.QueryRowContext(ctx, `SELECT path,identity,parent_task_id,size,modified,offset,model,tier,turn_id,history_ended,canonical_seen,previous_input,previous_cached,previous_cache_write,previous_output,previous_reasoning,previous_total,pending_response_id,pending_input,pending_cached,pending_cache_write,pending_output,pending_reasoning,pending_total FROM native_usage_sources WHERE task_id=?`, source.TaskID).Scan(&previousPath, &previousIdentity, &previousParentTaskID, &previousSize, &modified, &cursor.Offset, &cursor.Model, &cursor.ServiceTier, &cursor.TurnID, &cursor.ChildHistoryEnded, &cursor.CanonicalUsageSeen, &input, &cached, &cacheWrite, &output, &reasoning, &total, &cursor.PendingResponseID, &pendingInput, &pendingCached, &pendingCacheWrite, &pendingOutput, &pendingReasoning, &pendingTotal)
		if err != nil && err != sql.ErrNoRows {
			return result, err
		}
		reset := err == sql.ErrNoRows || previousIdentity != source.Identity || previousPath != source.Path || source.Size < previousSize || (source.Size == previousSize && source.ModifiedAt.UnixNano() != modified)
		if !reset && source.Size == previousSize && source.ModifiedAt.UnixNano() == modified {
			continue
		}
		cursor.KnownParentTaskID = previousParentTaskID
		if reset {
			cursor = sessionReadCursor{KnownParentTaskID: previousParentTaskID}
		} else if input.Valid {
			cursor.PreviousTotals = &tokenUsage{InputTokens: input.Int64, CachedInputTokens: cached.Int64, OutputTokens: output.Int64, ReasoningOutputTokens: reasoning.Int64, TotalTokens: total.Int64}
			if cacheWrite.Valid {
				cursor.PreviousTotals.CacheWriteInputTokens = &cacheWrite.Int64
			}
		}
		if !reset && pendingInput.Valid {
			cursor.PendingUsage = &tokenUsage{InputTokens: pendingInput.Int64, CachedInputTokens: pendingCached.Int64, OutputTokens: pendingOutput.Int64, ReasoningOutputTokens: pendingReasoning.Int64, TotalTokens: pendingTotal.Int64}
			if pendingCacheWrite.Valid {
				cursor.PendingUsage.CacheWriteInputTokens = &pendingCacheWrite.Int64
			}
		}
		parsed, readErr := read(ctx, source, cursor)
		if readErr != nil {
			return result, readErr
		}
		if err = ledger.saveSessionFile(ctx, source, parsed, reset); err != nil {
			return result, err
		}
		result.FilesRead++
		result.BytesRead += parsed.BytesRead
		for _, observation := range parsed.Observations {
			if observation.Usage != nil {
				result.UsageObservations++
			}
		}
	}
	return result, nil
}

func tokenArguments(usage *tokenUsage) []any {
	if usage == nil {
		return []any{nil, nil, nil, nil, nil, nil}
	}
	return []any{usage.InputTokens, usage.CachedInputTokens, usage.CacheWriteInputTokens, usage.OutputTokens, usage.ReasoningOutputTokens, usage.TotalTokens}
}
func windowArguments(window *sessionAllowanceWindow) []any {
	if window == nil {
		return []any{nil, nil, nil}
	}
	return []any{window.UsedPercent, window.WindowMinutes, window.ResetsAt.UnixNano()}
}
func (ledger *UsageLedger) saveSessionFile(ctx context.Context, source sessionFile, parsed sessionReadResult, reset bool) error {
	transaction, err := ledger.sqlDatabase.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if reset {
		if _, err = transaction.ExecContext(ctx, `DELETE FROM native_usage_observations WHERE task_id=?`, source.TaskID); err != nil {
			return err
		}
		if _, err = transaction.ExecContext(ctx, `DELETE FROM native_task_child_records WHERE source_task_id=?`, source.TaskID); err != nil {
			return err
		}
	}
	for _, child := range parsed.ChildTasks {
		if _, err = transaction.ExecContext(ctx, `INSERT OR REPLACE INTO native_task_child_records VALUES (?,?,?)`, source.TaskID, child.SourceOffset, child.TaskID); err != nil {
			return fmt.Errorf("save native child relationship: %w", err)
		}
	}
	for _, observation := range parsed.Observations {
		arguments := []any{observation.TaskID, observation.SourceOffset, observation.ResponseID, observation.RootTurnID, observation.SessionID, observation.TurnID, observation.At.UnixNano(), observation.Model, observation.ServiceTier}
		arguments = append(arguments, tokenArguments(observation.Usage)...)
		if allowance := observation.Allowance; allowance != nil {
			arguments = append(arguments, allowance.LimitID, allowance.PlanType)
			arguments = append(arguments, windowArguments(allowance.Primary)...)
			arguments = append(arguments, windowArguments(allowance.Secondary)...)
		} else {
			arguments = append(arguments, nil, nil, nil, nil, nil, nil, nil, nil)
		}
		if _, err = transaction.ExecContext(ctx, `INSERT OR REPLACE INTO native_usage_observations VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, arguments...); err != nil {
			return fmt.Errorf("save native observation: %w", err)
		}
	}
	cursor := parsed.Cursor
	arguments := []any{source.TaskID, source.Path, source.Identity, source.Size, source.ModifiedAt.UnixNano(), cursor.Offset, cursor.Model, cursor.ServiceTier, cursor.TurnID, cursor.ChildHistoryEnded, cursor.CanonicalUsageSeen}
	arguments = append(arguments, tokenArguments(cursor.PreviousTotals)...)
	arguments = append(arguments, parsed.Metadata.ParentTaskID, parsed.Metadata.ForkedFromTaskID, parsed.Metadata.CreatedAt.UnixNano(), parsed.Metadata.SourceKind, cursor.PendingResponseID)
	arguments = append(arguments, tokenArguments(cursor.PendingUsage)...)
	if _, err = transaction.ExecContext(ctx, `INSERT OR REPLACE INTO native_usage_sources VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, arguments...); err != nil {
		return fmt.Errorf("save native cursor: %w", err)
	}
	return transaction.Commit()
}

func (ledger *UsageLedger) readSessionUsageObservations(ctx context.Context, start, end time.Time) ([]sessionUsageObservation, error) {
	rows, err := ledger.sqlDatabase.QueryContext(ctx, `SELECT o.task_id,s.path,s.source_kind,o.source_offset,o.response_id,o.root_turn_id,o.session_id,o.turn_id,o.observed,o.model,o.tier,o.input,o.cached,o.cache_write,o.output,o.reasoning,o.total,o.limit_id,o.plan_type,o.primary_used,o.primary_minutes,o.primary_resets,o.secondary_used,o.secondary_minutes,o.secondary_resets FROM native_usage_observations o JOIN native_usage_sources s ON o.task_id=s.task_id WHERE o.observed>=? AND o.observed<? ORDER BY o.observed,o.task_id,o.source_offset`, start.UnixNano(), end.UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var observations []sessionUsageObservation
	for rows.Next() {
		var observation sessionUsageObservation
		var observed int64
		var input, cached, cacheWrite, output, reasoning, total, primaryMinutes, primaryResets, secondaryMinutes, secondaryResets sql.NullInt64
		var limitID, planType sql.NullString
		var primaryUsed, secondaryUsed sql.NullFloat64
		if err = rows.Scan(&observation.TaskID, &observation.SourcePath, &observation.SourceKind, &observation.SourceOffset, &observation.ResponseID, &observation.RootTurnID, &observation.SessionID, &observation.TurnID, &observed, &observation.Model, &observation.ServiceTier, &input, &cached, &cacheWrite, &output, &reasoning, &total, &limitID, &planType, &primaryUsed, &primaryMinutes, &primaryResets, &secondaryUsed, &secondaryMinutes, &secondaryResets); err != nil {
			return nil, err
		}
		observation.At = time.Unix(0, observed)
		if input.Valid {
			observation.Usage = &tokenUsage{InputTokens: input.Int64, CachedInputTokens: cached.Int64, OutputTokens: output.Int64, ReasoningOutputTokens: reasoning.Int64, TotalTokens: total.Int64}
			if cacheWrite.Valid {
				observation.Usage.CacheWriteInputTokens = &cacheWrite.Int64
			}
		}
		if limitID.Valid {
			observation.Allowance = &sessionAllowance{LimitID: limitID.String, PlanType: planType.String}
			if primaryUsed.Valid {
				observation.Allowance.Primary = &sessionAllowanceWindow{UsedPercent: primaryUsed.Float64, WindowMinutes: primaryMinutes.Int64, ResetsAt: time.Unix(0, primaryResets.Int64)}
			}
			if secondaryUsed.Valid {
				observation.Allowance.Secondary = &sessionAllowanceWindow{UsedPercent: secondaryUsed.Float64, WindowMinutes: secondaryMinutes.Int64, ResetsAt: time.Unix(0, secondaryResets.Int64)}
			}
		}
		observations = append(observations, observation)
	}
	return observations, rows.Err()
}
func (ledger *UsageLedger) readTaskMetadata(ctx context.Context) ([]taskMetadata, error) {
	rows, err := ledger.sqlDatabase.QueryContext(ctx, `SELECT s.task_id,s.parent_task_id,s.forked_from_task_id,s.created,s.source_kind,
 COALESCE(c.parent_task_id,''),COALESCE(c.parent_count,0)
 FROM native_usage_sources s LEFT JOIN (
  SELECT child_task_id,MIN(source_task_id) AS parent_task_id,COUNT(DISTINCT source_task_id) AS parent_count
  FROM native_task_child_records GROUP BY child_task_id
 ) c ON c.child_task_id=s.task_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var metadata []taskMetadata
	for rows.Next() {
		var item taskMetadata
		var created int64
		var recordedParent string
		var parentCount int
		if err = rows.Scan(&item.TaskID, &item.ParentTaskID, &item.ForkedFromTaskID, &created, &item.SourceKind, &recordedParent, &parentCount); err != nil {
			return nil, err
		}
		if item.ParentTaskID == "" {
			if parentCount > 1 {
				return nil, fmt.Errorf("conflicting native parent records for task %s", item.TaskID)
			}
			item.ParentTaskID = recordedParent
		}
		item.CreatedAt = time.Unix(0, created)
		metadata = append(metadata, item)
	}
	return metadata, rows.Err()
}
