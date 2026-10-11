package piusage_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/josh/codex-switch/internal/accounting"
	"github.com/josh/codex-switch/internal/codexusage"
	"github.com/josh/codex-switch/internal/piusage"
	"github.com/josh/codex-switch/internal/store"
	"github.com/josh/codex-switch/internal/taskidentity"
)

func TestNativePiUsageImportsOnceAndJoinsItsRoutedResponse(t *testing.T) {
	directory := t.TempDir()
	sessions := filepath.Join(directory, "sessions")
	if err := os.Mkdir(sessions, 0700); err != nil {
		t.Fatal(err)
	}
	// This shape and these counts come from the real Pi WebSocket continuity run.
	header := `{"type":"session","version":3,"id":"01a00000-0000-7000-8000-000000000001","timestamp":"2026-09-06T10:51:46.351Z"}` + "\n"
	response := `{"type":"message","id":"entry","timestamp":"2026-09-06T10:51:55Z","message":{"role":"assistant","provider":"openai-codex","model":"gpt-5.6-luna","responseId":"response","usage":{"input":568,"output":73,"cacheRead":4608,"cacheWrite":0,"reasoning":22,"totalTokens":5249}}}` + "\n"
	path := filepath.Join(sessions, "session.jsonl")
	if err := os.WriteFile(path, []byte(header+response[:len(response)-1]), 0600); err != nil {
		t.Fatal(err)
	}
	// Pi's reviewer exports also live under sessions, but are not session records.
	if err := os.WriteFile(filepath.Join(sessions, "context.jsonl"), []byte("{\"role\":\"user\",\"timestamp\":1786878875526}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	database, err := store.OpenStoreDatabase(filepath.Join(directory, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.CloseStoreDatabase()
	ctx := context.Background()
	imported, err := database.ImportPiUsage(ctx, sessions, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if imported.FilesDiscovered != 1 || imported.UsageObservations != 0 {
		t.Fatalf("partial import: %+v", imported)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	imported, err = database.ImportPiUsage(ctx, sessions, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if imported.UsageObservations != 1 {
		t.Fatalf("completed import: %+v", imported)
	}
	repeated, err := database.ImportPiUsage(ctx, sessions, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.BytesRead != 0 {
		t.Fatalf("unchanged import: %+v", repeated)
	}
	observations, err := database.ReadNativeUsageObservations(ctx, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 {
		t.Fatalf("observations: %d", len(observations))
	}
	observed := observations[0]
	if observed.TaskID != "f93bcdbe-7619-834b-9e1e-c6de8ec66b67" || observed.Usage.InputTokens != 5176 || observed.Usage.CachedInputTokens != 4608 || observed.Usage.CacheWriteInputTokens != nil {
		t.Fatalf("native observation: %+v usage=%+v", observed, observed.Usage)
	}
	nativeOnly, err := accounting.ReconcileCompletions(observations, nil)
	if err != nil {
		t.Fatal(err)
	}
	if nativeOnly[0].Cost != nil {
		t.Fatal("Pi's unrecorded service tier was priced")
	}
	zero := int64(0)
	joined, err := accounting.ReconcileCompletions(observations, []store.CompletedResponseRecord{{CompletedResponseID: "response", CodexTaskID: store.CodexTaskID(observed.TaskID), AccountName: "account-2", ModelSlug: "gpt-5.6-luna", HarnessMode: "standard", InputTokens: 5176, CachedInputTokens: 4608, CacheWriteInputTokens: &zero, OutputTokens: 73, ReasoningTokens: 22, CompletedAt: observed.At}})
	if err != nil {
		t.Fatal(err)
	}
	if len(joined) != 1 || joined[0].Tokens != 5249 || joined[0].AccountName != "account-2" || joined[0].Cost == nil || *joined[0].Cost != 293360 {
		t.Fatalf("joined response: %+v", joined)
	}
}

func TestPiForkFirstImportAfterStandardParentRemoval(t *testing.T) {
	for _, test := range []struct {
		name, parentFilename, knownParent string
		wantError                         bool
	}{
		{name: "standard Pi filename", parentFilename: "2026-09-06T11-02-55-196Z_01a00000-0000-7000-8000-000000000002.jsonl"},
		{name: "explicit Pi session ID", parentFilename: "2026-09-06T11-02-55-196Z_my_session.v2.jsonl"},
		{name: "unknown custom path", parentFilename: "01a00000-0000-7000-8000-000000000002.jsonl", wantError: true},
		{name: "previously imported custom path", parentFilename: "custom.jsonl", knownParent: "persisted-parent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "child.jsonl")
			header, err := json.Marshal(map[string]any{"type": "session", "id": "child", "timestamp": "2026-09-06T11:03:00Z", "parentSession": filepath.Join(directory, test.parentFilename)})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, append(header, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			sources, err := piusage.Discover(directory, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := piusage.Read(context.Background(), sources[0], codexusage.Cursor{KnownParentTaskID: test.knownParent})
			if test.wantError {
				if err == nil {
					t.Fatal("missing arbitrary parent was assigned an identity")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantParent := test.knownParent
			if wantParent == "" {
				parentSessionID := strings.TrimSuffix(strings.SplitN(test.parentFilename, "_", 2)[1], ".jsonl")
				wantParent = taskidentity.PiSession(parentSessionID)
			}
			if result.Metadata.ParentTaskID != wantParent || result.Cursor.KnownParentTaskID != wantParent {
				t.Fatalf("parent identity: metadata=%q cursor=%q want=%q", result.Metadata.ParentTaskID, result.Cursor.KnownParentTaskID, wantParent)
			}
		})
	}
}

func TestPiParentRecordsImportIncrementallyAndRespectHeaderAncestry(t *testing.T) {
	directory := t.TempDir()
	childPath := filepath.Join(directory, "child.jsonl")
	parentPath := filepath.Join(directory, "parent.jsonl")
	otherParentPath := filepath.Join(directory, "other-parent.jsonl")
	writeSession := func(path, id, parent string) {
		t.Helper()
		header, err := json.Marshal(map[string]any{"type": "session", "id": id, "timestamp": "2026-08-09T13:42:40Z", "parentSession": parent})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(header, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	appendChildRecord := func(path string) {
		t.Helper()
		record, err := json.Marshal(map[string]any{"type": "custom", "timestamp": "2026-08-09T13:42:44Z", "customType": "luna:subagent-record", "data": map[string]string{"session_file": childPath}})
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err = file.Write(append(record, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	writeSession(childPath, "child", "")
	writeSession(parentPath, "parent", "")
	database, err := store.OpenStoreDatabase(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.CloseStoreDatabase()
	ctx := context.Background()
	importSessions := func() codexusage.ImportResult {
		t.Helper()
		result, err := database.ImportPiUsage(ctx, directory, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	checkParent := func() {
		t.Helper()
		metadata, err := database.ReadNativeTaskMetadata(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range metadata {
			if item.TaskID == taskidentity.PiSession("child") {
				if item.ParentTaskID != taskidentity.PiSession("parent") {
					t.Fatalf("child parent: %q", item.ParentTaskID)
				}
				return
			}
		}
		t.Fatal("child metadata absent")
	}
	importSessions()
	appendChildRecord(parentPath)
	if result := importSessions(); result.FilesRead != 1 {
		t.Fatalf("incremental import: %+v", result)
	}
	checkParent()
	if result := importSessions(); result.BytesRead != 0 {
		t.Fatalf("unchanged import: %+v", result)
	}
	writeSession(otherParentPath, "other-parent", "")
	appendChildRecord(otherParentPath)
	importSessions()
	if _, err := database.ReadNativeTaskMetadata(ctx); err == nil {
		t.Fatal("incompatible parent evidence was silently accepted")
	}
	writeSession(childPath, "child", parentPath)
	importSessions()
	checkParent()
	writeSession(childPath, "child", "")
	writeSession(otherParentPath, "other-parent", "")
	importSessions()
	checkParent()
}
