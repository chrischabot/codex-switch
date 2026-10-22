package usage

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCodexSessionDiscoveryMatchesImportedFilesAfterChanges(t *testing.T) {
	ledger := openTestUsageLedger(t)
	home := t.TempDir()
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, taskID string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"type":"session_meta","timestamp":"2026-10-01T00:00:00Z","payload":{"id":"`+taskID+`","source":"cli"}}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(home, "sessions", "session.jsonl")
	write(path, "first-task")
	write(filepath.Join(home, "archived_sessions", "duplicate.jsonl"), "first-task")
	since := time.Unix(0, 0)
	if _, err := ledger.importCodexSessions(context.Background(), home, since); err != nil {
		t.Fatal(err)
	}
	imported, err := ledger.readImportedSessionFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		want, err := discoverCodexSessions(home, since, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := discoverCodexSessions(home, since, imported)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("discovery with imported sources differs: got %+v, want %+v", got, want)
		}
	}
	check()
	write(path, "other-task")
	if err := os.Chtimes(path, imported[path].ModifiedAt.Add(time.Second), imported[path].ModifiedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	check()
	replacement := path + ".replacement"
	write(replacement, "third-task")
	if err := os.Chtimes(replacement, imported[path].ModifiedAt, imported[path].ModifiedAt); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	check()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("{}\n"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	check()
	write(path, "second-task")
	check()
	if err := os.Rename(path, filepath.Join(home, "archived_sessions", "moved.jsonl")); err != nil {
		t.Fatal(err)
	}
	check()
	write(path, "replacement-task")
	check()
}
