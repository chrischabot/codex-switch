package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
