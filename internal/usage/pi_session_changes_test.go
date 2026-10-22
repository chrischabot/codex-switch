package usage

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joshp123/codex-switch/internal/taskidentity"
)

func writePiChangeSession(t *testing.T, path, id string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("{\"type\":\"session\",\"id\":%q,\"timestamp\":\"2026-10-22T10:00:00Z\",\"cwd\":\"/project\"}\n", id)), 0600); err != nil {
		t.Fatal(err)
	}
}

func appendPiChangeMessage(t *testing.T, path, response, provider, title string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(file, "{\"type\":\"message\",\"timestamp\":\"2026-10-22T10:00:01Z\",\"message\":{\"role\":\"assistant\",\"provider\":%q,\"model\":\"gpt-5.6-luna\",\"responseId\":%q,\"usage\":{\"input\":10,\"output\":2,\"totalTokens\":12}}}\n", provider, response)
	if err == nil && title != "" {
		_, err = fmt.Fprintf(file, "{\"type\":\"session_info\",\"name\":%q}\n", title)
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append: %v %v", err, closeErr)
	}
}

func piChangeObservationCount(t *testing.T, ledger *UsageLedger, response string) int {
	t.Helper()
	var count int
	if err := ledger.sqlDatabase.QueryRow(`SELECT COUNT(*) FROM native_usage_observations WHERE response_id=?`, response).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPiProxyChangesImportKnownAndNewSessionsAndAuditMisses(t *testing.T) {
	ledger := openTestUsageLedger(t)
	ctx := context.Background()
	directory := t.TempDir()
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", directory)
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)
	path := filepath.Join(directory, "project", "arbitrary-name.jsonl")
	writePiChangeSession(t, path, "known")
	cycle := func() {
		t.Helper()
		if err := ledger.importPiSessions(ctx, directory, since, now); err != nil {
			t.Fatal(err)
		}
	}
	cycle()
	startupWalk := ledger.piDiscovery.LastWalk
	if startupWalk.IsZero() || ledger.piDiscovery.WalkOnly {
		t.Fatal("startup did not establish response-driven discovery")
	}
	sentinel := filepath.Join(directory, "unrelated.jsonl")
	if err := os.WriteFile(sentinel, []byte("invalid session metadata\n"), 0600); err != nil {
		t.Fatal(err)
	}
	appendPiChangeMessage(t, path, "known-response", "openai-codex", "Known title")
	recordFoldProxy(t, ledger, "known-response", taskidentity.PiSession("known"), now, 10, 2)
	now = now.Add(time.Minute)
	cycle()
	if ledger.piDiscovery.LastWalk != startupWalk || piChangeObservationCount(t, ledger, "known-response") != 1 {
		t.Fatal("known response did not import without walking")
	}
	state := refreshTestFold(t, ledger, now)
	metadata, err := ledger.foldMetadata(state)
	if err != nil {
		t.Fatal(err)
	}
	if metadata(taskidentity.PiSession("known")).Title != "Known title" {
		t.Fatal("recorded session's title did not refresh")
	}
	t.Log("known session: next cycle imported response and title without walking unrelated files")
	if err := os.Rename(sentinel, sentinel+".ignored"); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(directory, "nested", "deeper", "custom.jsonl")
	writePiChangeSession(t, newPath, "new")
	appendPiChangeMessage(t, newPath, "new-response", "openai-codex", "New title")
	recordFoldProxy(t, ledger, "new-response", taskidentity.PiSession("new"), now, 10, 2)
	now = now.Add(time.Minute)
	cycle()
	if piChangeObservationCount(t, ledger, "new-response") != 1 {
		t.Fatal("new session was not found by fallback walk")
	}

	compareWalk := func() {
		t.Helper()
		reference := openTestUsageLedger(t)
		sources, err := reference.discoverPiSessions(directory, since)
		if err != nil {
			t.Fatal(err)
		}
		if err := reference.importSessionFiles(ctx, sources, reference.readPiSession); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(nativeProofRows(t, ledger), nativeProofRows(t, reference)) {
			t.Fatal("incremental imports differ from full-walk imports")
		}
	}
	compareWalk()
	t.Log("new session: next cycle walked once and imported arbitrary nested filename; native rows match full walk")
	// No new completion: a later append must be found by the hourly audit.
	appendPiChangeMessage(t, path, "unannounced-response", "openai-codex", "Audit title")
	now = now.Add(time.Minute)
	cycle()
	if piChangeObservationCount(t, ledger, "unannounced-response") != 0 {
		t.Fatal("unannounced response was read before the audit")
	}
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	now = now.Add(time.Hour)
	cycle()
	if !ledger.piDiscovery.WalkOnly || piChangeObservationCount(t, ledger, "unannounced-response") != 1 || !strings.Contains(logs.String(), "walking session folders every cycle until restart") {
		t.Fatalf("audit did not log, import and latch walking: %s", logs.String())
	}
	appendPiChangeMessage(t, path, "after-audit", "openai-codex", "")
	now = now.Add(time.Minute)
	cycle()
	if piChangeObservationCount(t, ledger, "after-audit") != 1 {
		t.Fatal("audit fallback did not keep walking")
	}
	compareWalk()
	t.Log("hourly audit: logged unannounced change, imported it, and kept walking next cycle; native rows match full walk")
}

func TestPiCompletionBeforeFileAppendIsRetriedForBothProviders(t *testing.T) {
	for _, provider := range []string{"openai-codex", "anthropic"} {
		t.Run(provider, func(t *testing.T) {
			ledger := openTestUsageLedger(t)
			ctx := context.Background()
			directory := t.TempDir()
			t.Setenv("PI_CODING_AGENT_SESSION_DIR", directory)
			path := filepath.Join(directory, "2026-10-22T10-00-00-000Z_delayed.jsonl")
			now := time.Now()
			since := now.Add(-24 * time.Hour)
			writePiChangeSession(t, path, "delayed")
			cycle := func() {
				t.Helper()
				if err := ledger.importPiSessions(ctx, directory, since, now); err != nil {
					t.Fatal(err)
				}
				now = now.Add(time.Minute)
			}
			cycle()
			walk := ledger.piDiscovery.LastWalk
			task := taskidentity.PiSession("delayed")
			if provider == "anthropic" {
				task = "claude:pi:delayed"
			}
			recordID := "delayed-response"
			if provider == "anthropic" {
				recordID = "claude:" + recordID
			}
			recordFoldProxy(t, ledger, recordID, task, now, 10, 2)
			cycle()
			appendPiChangeMessage(t, path, "delayed-response", provider, "Delayed title")
			cycle()
			if ledger.piDiscovery.LastWalk != walk {
				t.Fatal("known delayed session caused a walk")
			}
			if provider == "openai-codex" && piChangeObservationCount(t, ledger, "delayed-response") != 1 {
				t.Fatal("late native response was lost")
			}
			if provider == "anthropic" && piChangeObservationCount(t, ledger, "delayed-response") != 0 {
				t.Fatal("Claude native usage was unexpectedly imported")
			}
			title, err := ledger.piSessionTitle(path)
			if err != nil || title != "Delayed title" {
				t.Fatalf("late title: %q %v", title, err)
			}

			state := refreshTestFold(t, ledger, now)
			metadata, err := ledger.foldMetadata(state)
			if err != nil {
				t.Fatal(err)
			}
			if metadata(task).Title != "Delayed title" {
				t.Fatalf("reported title: %q", metadata(task).Title)
			}

			if provider == "anthropic" {
				if len(ledger.piDiscovery.Pending) == 0 {
					t.Fatal("Claude notification acknowledged without an imported native observation")
				}
				now = now.Add(5 * time.Minute)
			}
			cycle()
			if len(ledger.piDiscovery.Pending) != 0 {
				t.Fatal("notification neither acknowledged from imported usage nor expired after the read")
			}
			t.Log("response recorded before append: later cycle imported title, preserved provider counting, and settled notification without walking")
		})
	}
}

func TestPiPendingUsesImportedResponsesWithoutRereadingHistory(t *testing.T) {
	ledger := openTestUsageLedger(t)
	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	now := time.Now()
	writePiChangeSession(t, path, "pending")
	appendPiChangeMessage(t, path, "already-imported", "openai-codex", "")
	cycle := func() {
		t.Helper()
		if err := ledger.importPiSessions(ctx, directory, time.Time{}, now); err != nil {
			t.Fatal(err)
		}
	}
	cycle()
	// Leave the header intact but make previously imported history unreadable.
	// A new append is read from the stored cursor; an offset-zero reread fails.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	offset := bytes.IndexByte(data, '\n') + 1
	file, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteAt([]byte("!"), int64(offset)); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	recordFoldProxy(t, ledger, "already-imported", taskidentity.PiSession("pending"), now, 10, 2)
	recordFoldProxy(t, ledger, "never-saved", taskidentity.PiSession("pending"), now, 10, 2)
	appendPiChangeMessage(t, path, "new-message", "openai-codex", "")
	now = now.Add(time.Minute)
	cycle()
	task := taskidentity.PiSession("pending")
	if len(ledger.piDiscovery.Pending[task]) != 1 {
		t.Fatal("imported response was not acknowledged from database")
	}
	if _, found := ledger.piDiscovery.Pending[task]["never-saved"]; !found {
		t.Fatal("unsaved response dropped before five minutes")
	}
	appendPiChangeMessage(t, path, "later-growth", "openai-codex", "")
	now = now.Add(time.Minute)
	cycle()
	if piChangeObservationCount(t, ledger, "later-growth") != 1 {
		t.Fatal("new bytes were not imported incrementally")
	}
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	now = now.Add(3 * time.Minute)
	cycle()
	if len(ledger.piDiscovery.Pending) != 0 {
		t.Fatal("read unsaved response did not expire at five minutes")
	}
	if logs.Len() != 0 {
		t.Fatalf("expiry logged: %s", logs.String())
	}
	t.Log("imported response acknowledged by SQL; growing file never reread from zero; unsaved response expired silently at five minutes")
}

func TestPiPendingExpiryRequiresReadingSessionAfterResponse(t *testing.T) {
	ledger := openTestUsageLedger(t)
	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	now := time.Now()
	writePiChangeSession(t, path, "unread")
	cycle := func() {
		t.Helper()
		if err := ledger.importPiSessions(ctx, directory, time.Time{}, now); err != nil {
			t.Fatal(err)
		}
	}
	cycle()
	recordFoldProxy(t, ledger, "never-saved", taskidentity.PiSession("unread"), now, 10, 2)
	now = now.Add(6 * time.Minute)
	cycle()
	if len(ledger.piDiscovery.Pending) == 0 {
		t.Fatal("notification expired despite no session read after response")
	}
	appendPiChangeMessage(t, path, "later-message", "openai-codex", "")
	now = now.Add(time.Minute)
	cycle()
	if len(ledger.piDiscovery.Pending) != 0 {
		t.Fatal("old notification did not expire after session was read")
	}
	t.Log("five-minute age alone did not expire an unread session; its first subsequent read permitted expiry")
}

// A Pi run that keeps no session file still has its responses recorded by the
// proxy. The walk its unknown task causes settles it, so walking stops.
func TestPiResponseWithoutSessionFileStopsCausingWalks(t *testing.T) {
	ledger := openTestUsageLedger(t)
	ctx := context.Background()
	directory := t.TempDir()
	writePiChangeSession(t, filepath.Join(directory, "session.jsonl"), "saved")
	now := time.Now()
	cycle := func() {
		t.Helper()
		if err := ledger.importPiSessions(ctx, directory, time.Time{}, now); err != nil {
			t.Fatal(err)
		}
	}
	cycle()
	recordFoldProxy(t, ledger, "sessionless-response", taskidentity.PiSession("no-file"), now.Add(-time.Second), 10, 2)
	cycle()
	now = now.Add(6 * time.Minute)
	cycle()
	if len(ledger.piDiscovery.Pending) != 0 {
		t.Fatal("a response from a Pi run with no session file stayed pending")
	}
	walked := ledger.piDiscovery.LastWalk
	now = now.Add(time.Minute)
	cycle()
	if ledger.piDiscovery.LastWalk != walked {
		t.Fatal("a settled response from a Pi run with no session file still causes walks")
	}
}
