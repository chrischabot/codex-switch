package usage

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// CODEX_SWITCH_INDEX_DATABASE selects a disposable backup of a real Codex
// state database. Its schema, indexes and triggers are retained in the proof.
func TestCodexThreadIndexImportsRealFiles(t *testing.T) {
	home := t.TempDir()
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	index := openCodexIndexFixture(t, home)
	ledger, reference := openTestUsageLedger(t), openTestUsageLedger(t)
	ctx := context.Background()
	base := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	since := base.Add(-24 * time.Hour)
	pi := t.TempDir()
	path := filepath.Join(home, "sessions", "first.jsonl")
	writeIndexedRollout(t, path, "first")
	upsertIndexedRollout(t, index, "first", path, base.UnixMilli(), false)
	cycle := func() {
		t.Helper()
		if err := ledger.importNativeSessions(ctx, home, pi, since); err != nil {
			t.Fatal(err)
		}
	}
	cycle()
	if !ledger.codexDiscovery.Started || ledger.codexDiscovery.WalkOnly {
		t.Fatal("startup did not establish a trusted index")
	}
	startupWalk := ledger.codexDiscovery.LastWalk
	// A walk would try to parse this file and fail. Indexed cycles must never
	// encounter it; the separate walk reference hides it before comparison.
	sentinel := filepath.Join(home, "sessions", "not-indexed.jsonl")
	if err := os.WriteFile(sentinel, []byte("not session metadata\n"), 0600); err != nil {
		t.Fatal(err)
	}
	compareWalk := func(stage string) {
		t.Helper()
		if err := os.Rename(sentinel, sentinel+".ignored"); err != nil {
			t.Fatal(err)
		}
		walked, err := discoverCodexSessions(home, since, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := reference.importSessionFiles(ctx, walked, readCodexSession); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(sentinel+".ignored", sentinel); err != nil {
			t.Fatal(err)
		}
		if got, want := nativeProofRows(t, ledger), nativeProofRows(t, reference); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s differs from walk: got %v want %v", stage, got, want)
		}
		if ledger.codexDiscovery.LastWalk != startupWalk {
			t.Fatalf("%s unexpectedly walked", stage)
		}
		t.Log(stage + ": next cycle matched walk; no session-folder walk")
	}
	appendIndexedUsage(t, path, "first", "append", 12)
	upsertIndexedRollout(t, index, "first", path, base.Add(time.Second).UnixMilli(), false)
	cycle()
	compareWalk("append")
	// A second append in the same rounded second must not disappear below the
	// high-water mark.
	appendIndexedUsage(t, path, "first", "same-second", 7)
	cycle()
	compareWalk("same rounded second")
	newPath := filepath.Join(home, "sessions", "new.jsonl")
	writeIndexedRollout(t, newPath, "new")
	appendIndexedUsage(t, newPath, "new", "new-response", 5)
	upsertIndexedRollout(t, index, "new", newPath, base.Add(2*time.Second).UnixMilli(), false)
	cycle()
	compareWalk("new thread")
	archived := filepath.Join(home, "archived_sessions", "first.jsonl")
	if err := os.Rename(path, archived); err != nil {
		t.Fatal(err)
	}
	upsertIndexedRollout(t, index, "first", archived, base.Add(3*time.Second).UnixMilli(), true)
	cycle()
	compareWalk("archive move")
	// Compression removes the file from today's importer; its already counted
	// usage remains, just as with the walk.
	if err := os.Rename(archived, archived+".zst"); err != nil {
		t.Fatal(err)
	}
	upsertIndexedRollout(t, index, "first", archived+".zst", base.Add(4*time.Second).UnixMilli(), true)
	cycle()
	compareWalk("compressed archive excluded")
	appendIndexedUsage(t, newPath, "new", "index-missed", 19)
	// new still has its old index timestamp, now outside the one-second overlap.
	cycle()
	var count int
	if err := ledger.sqlDatabase.QueryRow(`SELECT COUNT(*) FROM native_usage_observations WHERE response_id='index-missed'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("broken index unexpectedly reported append: count=%d error=%v", count, err)
	}
	if err := os.Rename(sentinel, sentinel+".ignored"); err != nil {
		t.Fatal(err)
	}
	ledger.codexDiscovery.LastWalk = time.Now().Add(-time.Hour)
	var logs bytes.Buffer
	previousLog := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousLog) })
	cycle()
	if !ledger.codexDiscovery.WalkOnly || !strings.Contains(logs.String(), "thread index missed changed rollout") {
		t.Fatalf("hourly check did not latch walking: %s", logs.String())
	}
	if err := ledger.sqlDatabase.QueryRow(`SELECT COUNT(*) FROM native_usage_observations WHERE response_id='index-missed'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("hourly walk lost missed append: count=%d error=%v", count, err)
	}
	appendIndexedUsage(t, newPath, "new", "walking-after-miss", 23)
	cycle()
	if err := ledger.sqlDatabase.QueryRow(`SELECT COUNT(*) FROM native_usage_observations WHERE response_id='walking-after-miss'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("walking did not remain latched: count=%d error=%v", count, err)
	}
	t.Log("hourly audit: logged omitted append, imported it, and kept walking on following cycle")
}

func TestCodexUnreadableIndexWalksAndRecovers(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	index := openCodexIndexFixture(t, home)
	ledger := openTestUsageLedger(t)
	ctx := context.Background()
	path := filepath.Join(home, "sessions", "thread.jsonl")
	writeIndexedRollout(t, path, "thread")
	now := time.Now()
	upsertIndexedRollout(t, index, "thread", path, now.UnixMilli(), false)
	pi := t.TempDir()
	cycle := func() {
		t.Helper()
		if err := ledger.importNativeSessions(ctx, home, pi, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	cycle()
	if _, err := index.Exec(`ALTER TABLE threads RENAME TO unavailable_threads`); err != nil {
		t.Fatal(err)
	}
	appendIndexedUsage(t, path, "thread", "fallback", 12)
	var logs bytes.Buffer
	previousLog := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousLog) })
	cycle()
	cycle()
	if strings.Count(logs.String(), "walking session folders because") != 1 {
		t.Fatalf("schema failure was not logged once: %s", logs.String())
	}
	if len(nativeProofRows(t, ledger)) != 1 {
		t.Fatal("schema fallback lost usage")
	}
	if _, err := index.Exec(`ALTER TABLE unavailable_threads RENAME TO threads`); err != nil {
		t.Fatal(err)
	}
	upsertIndexedRollout(t, index, "thread", path, now.Add(time.Second).UnixMilli(), false)
	cycle()
	if ledger.codexDiscovery.WalkOnly {
		t.Fatal("a readable index did not recover after temporary failure")
	}
	// Only the hourly audit can permanently reject an index. Recovery resumes
	// indexed discovery, without revisiting unrelated files.
	if err := os.WriteFile(filepath.Join(home, "sessions", "unrelated.jsonl"), []byte("invalid metadata\n"), 0600); err != nil {
		t.Fatal(err)
	}
	appendIndexedUsage(t, path, "thread", "recovered", 9)
	upsertIndexedRollout(t, index, "thread", path, now.Add(2*time.Second).UnixMilli(), false)
	cycle()
	if len(nativeProofRows(t, ledger)) != 2 {
		t.Fatal("recovered index lost appended usage")
	}
}

func openCodexIndexFixture(t *testing.T, home string) *sql.DB {
	t.Helper()
	path := filepath.Join(home, "state_5.sqlite")
	if source := os.Getenv("CODEX_SWITCH_INDEX_DATABASE"); source != "" {
		input, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		output, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(output, input)
		closeErr := output.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("copy state database: %v %v", err, closeErr)
		}
		t.Log("using copied real Codex schema, indexes and triggers")
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	_, err = database.Exec(`PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS threads (id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, updated_at_ms INTEGER, source TEXT NOT NULL, model_provider TEXT NOT NULL, cwd TEXT NOT NULL, title TEXT NOT NULL, sandbox_policy TEXT NOT NULL, approval_mode TEXT NOT NULL, archived INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS idx_threads_updated_at_ms ON threads(updated_at_ms DESC,id DESC);
DELETE FROM threads;`)
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func upsertIndexedRollout(t *testing.T, database *sql.DB, id, path string, updated int64, archived bool) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO threads(id,rollout_path,created_at,updated_at,updated_at_ms,source,model_provider,cwd,title,sandbox_policy,approval_mode,archived)
VALUES(?,?,0,?,?,'cli','openai','','','','',?) ON CONFLICT(id) DO UPDATE SET rollout_path=excluded.rollout_path,updated_at=excluded.updated_at,updated_at_ms=excluded.updated_at_ms,archived=excluded.archived`, id, path, updated/1000, updated, archived)
	if err != nil {
		t.Fatal(err)
	}
}

func writeIndexedRollout(t *testing.T, path, id string) {
	t.Helper()
	data := fmt.Sprintf("{\"type\":\"session_meta\",\"timestamp\":\"2026-10-22T10:00:00Z\",\"payload\":{\"id\":%q,\"source\":\"cli\"}}\n{\"type\":\"turn_context\",\"payload\":{\"model\":\"gpt-5.6-luna\"}}\n", id)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func appendIndexedUsage(t *testing.T, path, id, response string, tokens int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(file, "{\"type\":\"token_usage_record\",\"timestamp\":\"2026-10-22T10:00:01Z\",\"payload\":{\"thread_id\":%q,\"response_id\":%q,\"usage\":{\"input_tokens\":%d,\"output_tokens\":2}}}\n", id, response, tokens)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append usage: %v %v", err, closeErr)
	}
}

func nativeProofRows(t *testing.T, ledger *UsageLedger) []string {
	t.Helper()
	rows, err := ledger.sqlDatabase.Query(`SELECT task_id,source_offset,response_id,model,input,output FROM native_usage_observations ORDER BY task_id,source_offset`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var id, response, model string
		var offset, input, output int64
		if err := rows.Scan(&id, &offset, &response, &model, &input, &output); err != nil {
			t.Fatal(err)
		}
		result = append(result, fmt.Sprint(id, offset, response, model, input, output))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
