package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeTitleTracksAppendedRenamesAndCustomOverride(t *testing.T) {
	ledger := openTestUsageLedger(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	write := func(data string) {
		t.Helper()
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString(data); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	check := func(expected string) {
		t.Helper()
		title, err := ledger.claudeCodeSessionTitle(path, "session")
		if err != nil {
			t.Fatal(err)
		}
		if title != expected {
			t.Fatalf("title = %q, want %q", title, expected)
		}
	}
	write(`{"type":"user","sessionId":"session","message":{"content":"First prompt"}}` + "\n")
	check("First prompt")
	write(`{"type":"summary","summary":"Summary"}` + "\n")
	check("Summary")
	write(`{"type":"ai-title","aiTitle":"Generated"}` + "\n")
	check("Generated")
	write(`{"type":"custom-title","sessionId":"other","customTitle":"Other session"}` + "\n")
	check("Generated")
	write(`{"type":"custom-title","customTitle":"Renamed"}`)
	check("Renamed")
	write("\n" + `{"type":"custom-title","customTitle":""}` + "\n")
	check("Generated")
	if err := os.Mkdir(strings.TrimSuffix(path, ".jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(strings.TrimSuffix(path, ".jsonl"), "custom-title.json")
	if err := os.WriteFile(override, []byte(`{"customTitle":"Override"}`), 0600); err != nil {
		t.Fatal(err)
	}
	check("Override")
	if err := os.Rename(override, override+".saved"); err != nil {
		t.Fatal(err)
	}
	check("Generated")
	if err := os.WriteFile(path, []byte(`{"type":"user","message":{"content":"Replaced"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check("Replaced")
	var offset int64
	if err := ledger.sqlDatabase.QueryRow(`SELECT offset FROM session_titles WHERE path=?`, path).Scan(&offset); err != nil {
		t.Fatal(err)
	}
	check("Replaced")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if offset != info.Size() {
		t.Fatalf("imported offset %d, file size %d", offset, info.Size())
	}
}

func TestPiTitleTracksLatestRenameWithoutImportingToolOutput(t *testing.T) {
	ledger := openTestUsageLedger(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session","cwd":"/projects/example"}`+"\n"+strings.Repeat("x", 128*1024)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check := func(expected string) {
		t.Helper()
		actual, err := ledger.piSessionTitle(path)
		if err != nil {
			t.Fatal(err)
		}
		if actual != expected {
			t.Fatalf("title = %q, want %q", actual, expected)
		}
	}
	check("pi · example")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"type":"session_info","name":"Latest name"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	check("Latest name")
	if err := os.WriteFile(path, []byte(`{"type":"session","cwd":"/projects/replaced"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check("pi · replaced")
}

func TestSessionTitleCacheKeepsPartialLineSeparateFromAppendedState(t *testing.T) {
	ledger := openTestUsageLedger(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"custom-title","customTitle":"Partial"}`), 0600); err != nil {
		t.Fatal(err)
	}
	check := func(expected string) {
		t.Helper()
		title, err := ledger.claudeCodeSessionTitle(path, "session")
		if err != nil || title != expected {
			t.Fatalf("title = %q, %v; want %q", title, err, expected)
		}
	}
	check("Partial")
	check("Partial")
	// The previously valid tail becomes an invalid JSON line. It must not have
	// overwritten the complete-line state used for the next read.
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("invalid\n" + `{"type":"ai-title","aiTitle":"Generated"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	check("Generated")
}

func TestTitleCacheTracksSameSizeRewriteAndReplacement(t *testing.T) {
	ledger := openTestUsageLedger(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write := func(path, name string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"type":"custom-title","customTitle":"`+name+`"}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(expected string) {
		t.Helper()
		actual, err := ledger.claudeCodeSessionTitle(path, "session")
		if err != nil || actual != expected {
			t.Fatalf("title = %q, %v; want %q", actual, err, expected)
		}
	}
	write(path, "First")
	check("First")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	write(path, "Other")
	changed := info.ModTime().Add(time.Second)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	check("Other")
	replacement := path + ".replacement"
	write(replacement, "Final")
	if err := os.Chtimes(replacement, changed, changed); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	check("Final")
}

func TestCachedTitlePathsFindNewNestedFilesAndFollowSymlinks(t *testing.T) {
	ledger := openTestUsageLedger(t)
	directory := t.TempDir()
	project := filepath.Join(directory, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	check := func(session, expected string) {
		t.Helper()
		paths, err := ledger.cachedTitlePaths(directory, false, map[string]bool{session: true})
		if err != nil {
			t.Fatal(err)
		}
		if expected == "" {
			if len(paths[session]) != 0 {
				t.Fatalf("unexpected paths: %v", paths)
			}
		} else if len(paths[session]) != 1 || paths[session][0] != expected {
			t.Fatalf("paths: %v, want %s", paths, expected)
		}
	}
	check("session", "")
	rootInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "session.jsonl")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	// Preserve the root's timestamp: adding a nested file does not change it.
	if err := os.Chtimes(directory, rootInfo.ModTime(), rootInfo.ModTime()); err != nil {
		t.Fatal(err)
	}
	check("session", path)
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "linked.jsonl"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	check("linked", filepath.Join(link, "linked.jsonl"))
	// A removed known path must be rediscovered when that session moves.
	if err := os.Rename(path, filepath.Join(external, "session.jsonl")); err != nil {
		t.Fatal(err)
	}
	check("session", filepath.Join(link, "session.jsonl"))
}

func TestCachedPiHeadersTrackAppendReplacementAndMissingParent(t *testing.T) {
	ledger := openTestUsageLedger(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	write := func(path, id string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"type":"session","id":"`+id+`"}`+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, "first")
	check := func(expected string) {
		t.Helper()
		header, err := ledger.cachedPiSessionHeader(path)
		if err != nil || header.ID != expected {
			t.Fatalf("header = %+v, %v; want %s", header, err, expected)
		}
	}
	check("first")
	check("first")
	replacement := path + ".replacement"
	write(replacement, "other")
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	check("other")
	parent := filepath.Join(directory, "parent.jsonl")
	if _, err := ledger.cachedPiSessionHeader(parent); !os.IsNotExist(err) {
		t.Fatalf("missing header error = %v", err)
	}
	write(parent, "parent")
	header, err := ledger.cachedPiSessionHeader(parent)
	if err != nil || header.ID != "parent" {
		t.Fatalf("late parent = %+v, %v", header, err)
	}
}

func TestCachedTitlePathsPreserveDuplicateOrderWhenNewCopyAppears(t *testing.T) {
	ledger := openTestUsageLedger(t)
	directory := t.TempDir()
	first := filepath.Join(directory, "a")
	last := filepath.Join(directory, "z")
	for _, path := range []string{first, last} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	original := filepath.Join(first, "session.jsonl")
	if err := os.WriteFile(original, nil, 0600); err != nil {
		t.Fatal(err)
	}
	sessions := map[string]bool{"session": true}
	paths, err := ledger.cachedTitlePaths(directory, false, sessions)
	if err != nil || len(paths["session"]) != 1 {
		t.Fatalf("initial paths = %v, %v", paths, err)
	}
	duplicate := filepath.Join(last, "session.jsonl")
	if err := os.WriteFile(duplicate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	paths, err = ledger.cachedTitlePaths(directory, false, sessions)
	if err != nil || len(paths["session"]) != 2 || paths["session"][0] != original || paths["session"][1] != duplicate {
		t.Fatalf("duplicate paths = %v, %v", paths, err)
	}
}

func TestClaudeCustomTitleCacheTracksRewrite(t *testing.T) {
	ledger := openTestUsageLedger(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"ai-title","aiTitle":"Generated"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sidecarDirectory := strings.TrimSuffix(path, ".jsonl")
	if err := os.Mkdir(sidecarDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(sidecarDirectory, "custom-title.json")
	check := func(expected string) {
		t.Helper()
		title, err := ledger.claudeCodeSessionTitle(path, "session")
		if err != nil || title != expected {
			t.Fatalf("title = %q, %v; want %q", title, err, expected)
		}
	}
	for _, title := range []string{"First", "Other"} {
		if err := os.WriteFile(sidecar, []byte(`{"customTitle":"`+title+`"}`), 0600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(sidecar)
		if err != nil {
			t.Fatal(err)
		}
		modified := info.ModTime().Add(time.Second)
		if err := os.Chtimes(sidecar, modified, modified); err != nil {
			t.Fatal(err)
		}
		check(title)
		check(title)
	}
}
