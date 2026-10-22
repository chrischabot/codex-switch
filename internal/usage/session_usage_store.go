package usage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
-- Reconciliation reads the latest allowance for a response, rather than scanning history.
CREATE INDEX IF NOT EXISTS native_usage_response_id ON native_usage_observations(response_id) WHERE response_id!='' AND limit_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS native_task_child_records (
 source_task_id TEXT NOT NULL, source_offset INTEGER NOT NULL, child_task_id TEXT NOT NULL,
 PRIMARY KEY(source_task_id,source_offset)
) STRICT;
CREATE TABLE IF NOT EXISTS usage_mutation_counter (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), generation INTEGER NOT NULL
) STRICT;
INSERT OR IGNORE INTO usage_mutation_counter VALUES(1,0);
CREATE TRIGGER IF NOT EXISTS usage_completed_response_deleted AFTER DELETE ON completed_responses BEGIN
 UPDATE usage_mutation_counter SET generation=generation+1 WHERE singleton=1;
END;
CREATE TRIGGER IF NOT EXISTS usage_native_observation_deleted AFTER DELETE ON native_usage_observations BEGIN
 UPDATE usage_mutation_counter SET generation=generation+1 WHERE singleton=1;
END;
DROP TRIGGER IF EXISTS usage_native_source_inserted;
CREATE TRIGGER IF NOT EXISTS usage_native_source_deleted AFTER DELETE ON native_usage_sources BEGIN
 UPDATE usage_mutation_counter SET generation=generation+1 WHERE singleton=1;
END;
CREATE TRIGGER IF NOT EXISTS usage_native_child_deleted AFTER DELETE ON native_task_child_records BEGIN
 UPDATE usage_mutation_counter SET generation=generation+1 WHERE singleton=1;
END;
`)
	return err
}

// Read fingerprints only for the candidates discovered this cycle. Chunking
// keeps startup and hourly walks within SQLite's parameter limit.
func (ledger *UsageLedger) readImportedSources(ctx context.Context, sources []sessionFile) (map[string]sessionFile, error) {
	imported := make(map[string]sessionFile, len(sources))
	for start := 0; start < len(sources); start += 500 {
		batch := sources[start:min(start+500, len(sources))]
		arguments := make([]any, len(batch))
		for i, source := range batch {
			arguments[i] = source.TaskID
		}
		rows, err := ledger.sqlDatabase.QueryContext(ctx, `SELECT task_id,path,identity,size,modified FROM native_usage_sources WHERE task_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`)`, arguments...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var source sessionFile
			var modified int64
			if err := rows.Scan(&source.TaskID, &source.Path, &source.Identity, &source.Size, &modified); err != nil {
				rows.Close()
				return nil, err
			}
			source.ModifiedAt = time.Unix(0, modified)
			imported[source.Path] = source
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return imported, nil
}

func (ledger *UsageLedger) importNativeSessions(ctx context.Context, codexHome, piSessions string, since time.Time) error {
	// A failed import must not advance discovery past the bytes still unread.
	previousDiscovery := ledger.codexDiscovery
	codexSources, err := ledger.discoverCodexSessionChanges(ctx, codexHome, since, time.Now())
	if err != nil {
		ledger.codexDiscovery = previousDiscovery
		return err
	}
	if err = ledger.importSessionFiles(ctx, codexSources, readCodexSession); err != nil {
		ledger.codexDiscovery = previousDiscovery
		return err
	}
	return ledger.importPiSessions(ctx, piSessions, since, time.Now())
}

func (ledger *UsageLedger) importSessionFiles(ctx context.Context, sources []sessionFile, read func(context.Context, sessionFile, sessionReadCursor) (sessionReadResult, error)) error {
	imported, err := ledger.readImportedSources(ctx, sources)
	if err != nil {
		return err
	}
	return ledger.importSessionFilesWithKnownFiles(ctx, sources, read, imported)
}

func (ledger *UsageLedger) importSessionFilesWithKnownFiles(ctx context.Context, sources []sessionFile, read func(context.Context, sessionFile, sessionReadCursor) (sessionReadResult, error), importedByPath map[string]sessionFile) error {
	var err error
	for _, source := range sources {
		if previous, known := importedByPath[source.Path]; known && previous.TaskID == source.TaskID && previous.Identity == source.Identity && previous.Size == source.Size && previous.ModifiedAt.UnixNano() == source.ModifiedAt.UnixNano() {
			continue
		}
		var cursor sessionReadCursor
		var previousIdentity, previousPath, previousParentTaskID string
		var previousSize, modified int64
		var input, cached, cacheWrite, output, reasoning, total sql.NullInt64
		var pendingInput, pendingCached, pendingCacheWrite, pendingOutput, pendingReasoning, pendingTotal sql.NullInt64
		err = ledger.sqlDatabase.QueryRowContext(ctx, `SELECT path,identity,parent_task_id,size,modified,offset,model,tier,turn_id,history_ended,canonical_seen,previous_input,previous_cached,previous_cache_write,previous_output,previous_reasoning,previous_total,pending_response_id,pending_input,pending_cached,pending_cache_write,pending_output,pending_reasoning,pending_total FROM native_usage_sources WHERE task_id=?`, source.TaskID).Scan(&previousPath, &previousIdentity, &previousParentTaskID, &previousSize, &modified, &cursor.Offset, &cursor.Model, &cursor.ServiceTier, &cursor.TurnID, &cursor.ChildHistoryEnded, &cursor.CanonicalUsageSeen, &input, &cached, &cacheWrite, &output, &reasoning, &total, &cursor.PendingResponseID, &pendingInput, &pendingCached, &pendingCacheWrite, &pendingOutput, &pendingReasoning, &pendingTotal)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		var expectedSource *sessionFile
		if err == nil {
			expectedSource = &sessionFile{TaskID: source.TaskID, Path: previousPath, Identity: previousIdentity, Size: previousSize, ModifiedAt: time.Unix(0, modified)}
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
			return readErr
		}
		if err = ledger.saveSessionFile(ctx, source, parsed, reset, expectedSource); err != nil {
			return err
		}
	}
	return nil
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
func (ledger *UsageLedger) saveSessionFile(ctx context.Context, source sessionFile, parsed sessionReadResult, reset bool, expectedSource *sessionFile) error {
	transaction, err := ledger.sqlDatabase.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	// Acquire the writer lock before checking the cursor: another generation
	// may have imported this file while this process was reading its bytes.
	if _, err = transaction.ExecContext(ctx, `UPDATE usage_mutation_counter SET generation=generation WHERE singleton=1`); err != nil {
		return err
	}
	var current sessionFile
	var modified int64
	err = transaction.QueryRowContext(ctx, `SELECT path,identity,size,modified FROM native_usage_sources WHERE task_id=?`, source.TaskID).Scan(&current.Path, &current.Identity, &current.Size, &modified)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if expectedSource == nil {
		if err == nil {
			return nil
		}
	} else if err == sql.ErrNoRows || current.Path != expectedSource.Path || current.Identity != expectedSource.Identity || current.Size != expectedSource.Size || modified != expectedSource.ModifiedAt.UnixNano() {
		// The next discovery reads that generation's cursor and any bytes beyond it.
		return nil
	}
	if reset {
		if _, err = transaction.ExecContext(ctx, `DELETE FROM native_usage_observations WHERE task_id=?`, source.TaskID); err != nil {
			return err
		}
		if _, err = transaction.ExecContext(ctx, `DELETE FROM native_task_child_records WHERE source_task_id=?`, source.TaskID); err != nil {
			return err
		}
	}
	for _, child := range parsed.ChildTasks {
		if err = insertIdenticalNativeRow(ctx, transaction, "native_task_child_records", []string{"source_task_id", "source_offset", "child_task_id"}, []any{source.TaskID, child.SourceOffset, child.TaskID}, 2); err != nil {
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
		if err = insertIdenticalNativeRow(ctx, transaction, "native_usage_observations", nativeObservationColumns, arguments, 2); err != nil {
			return fmt.Errorf("save native observation: %w", err)
		}
	}
	cursor := parsed.Cursor
	arguments := []any{source.TaskID, source.Path, source.Identity, source.Size, source.ModifiedAt.UnixNano(), cursor.Offset, cursor.Model, cursor.ServiceTier, cursor.TurnID, cursor.ChildHistoryEnded, cursor.CanonicalUsageSeen}
	arguments = append(arguments, tokenArguments(cursor.PreviousTotals)...)
	arguments = append(arguments, parsed.Metadata.ParentTaskID, parsed.Metadata.ForkedFromTaskID, parsed.Metadata.CreatedAt.UnixNano(), parsed.Metadata.SourceKind, cursor.PendingResponseID)
	arguments = append(arguments, tokenArguments(cursor.PendingUsage)...)
	// Source cursor updates need a fresh, increasing rowid so readers see them
	// above their watermark, including when this source had the highest rowid.
	var nextSourceRowID int64
	if err = transaction.QueryRowContext(ctx, `SELECT COALESCE(MAX(rowid),0)+1 FROM native_usage_sources`).Scan(&nextSourceRowID); err != nil {
		return err
	}
	arguments = append([]any{nextSourceRowID}, arguments...)
	if _, err = transaction.ExecContext(ctx, `INSERT INTO native_usage_sources (rowid,task_id,path,identity,size,modified,offset,model,tier,turn_id,history_ended,canonical_seen,previous_input,previous_cached,previous_cache_write,previous_output,previous_reasoning,previous_total,parent_task_id,forked_from_task_id,created,source_kind,pending_response_id,pending_input,pending_cached,pending_cache_write,pending_output,pending_reasoning,pending_total) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(task_id) DO UPDATE SET rowid=excluded.rowid,path=excluded.path,identity=excluded.identity,size=excluded.size,modified=excluded.modified,offset=excluded.offset,model=excluded.model,tier=excluded.tier,turn_id=excluded.turn_id,history_ended=excluded.history_ended,canonical_seen=excluded.canonical_seen,previous_input=excluded.previous_input,previous_cached=excluded.previous_cached,previous_cache_write=excluded.previous_cache_write,previous_output=excluded.previous_output,previous_reasoning=excluded.previous_reasoning,previous_total=excluded.previous_total,parent_task_id=excluded.parent_task_id,forked_from_task_id=excluded.forked_from_task_id,created=excluded.created,source_kind=excluded.source_kind,pending_response_id=excluded.pending_response_id,pending_input=excluded.pending_input,pending_cached=excluded.pending_cached,pending_cache_write=excluded.pending_cache_write,pending_output=excluded.pending_output,pending_reasoning=excluded.pending_reasoning,pending_total=excluded.pending_total`, arguments...); err != nil {
		return fmt.Errorf("save native cursor: %w", err)
	}
	return transaction.Commit()
}
