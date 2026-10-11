package codexstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLookupFallsBackToOfficialLocalTaskFiles(t *testing.T) {
	home := t.TempDir()
	writeLocalNames(t, home,
		fmt.Sprintf(`{"id":%q,"thread_name":"Old parent name"}`, fixtureParent),
		fmt.Sprintf(`{"id":%q,"thread_name":"Parent task"}`, fixtureParent),
		fmt.Sprintf(`{"id":%q,"thread_name":"Child task"}`, fixtureChild),
	)
	writeLocalRollout(t, home, fixtureParent, fmt.Sprintf(
		`{"timestamp":"2026-07-14T10:00:00Z","type":"session_meta","payload":{"id":%q,"source":"vscode"}}`, fixtureParent,
	))
	writeLocalRollout(t, home, fixtureChild, fmt.Sprintf(
		`{"timestamp":"2026-07-14T10:01:00Z","type":"session_meta","payload":{"id":%q,"agent_nickname":"Pascal","source":{"subagent":{"thread_spawn":{"parent_thread_id":%q,"agent_nickname":"ignored fallback"}}}}}`, fixtureChild, fixtureParent,
	))

	reader := LocalReader{
		AppServerSocket:  filepath.Join(home, "missing.sock"),
		CodexHome:        home,
		AppServerTimeout: 10 * time.Millisecond,
	}
	got, err := reader.Lookup(t.Context(), []string{fixtureChild})
	if err != nil {
		t.Fatal(err)
	}
	if child := got[fixtureChild]; child.DisplayName != "Child task" || child.AgentNickname != "Pascal" ||
		child.ParentTaskID != fixtureParent || child.Relationship != TaskRelationshipKnownParent {
		t.Fatalf("child = %#v", child)
	} else if child.RuntimeStatus.State != "" {
		t.Fatalf("file fallback invented runtime status: %#v", child.RuntimeStatus)
	} else if len(child.Sources) != 1 || child.Sources[0] != TaskMetadataFromLocalFiles {
		t.Fatalf("file fallback source = %#v", child.Sources)
	}
	if parent := got[fixtureParent]; parent.DisplayName != "Parent task" || parent.ParentTaskID != "" ||
		parent.Relationship != TaskRelationshipKnownRoot {
		t.Fatalf("parent = %#v", parent)
	}
}

func TestLookupDoesNotGuessRootFromSessionIndexAlone(t *testing.T) {
	home := t.TempDir()
	writeLocalNames(t, home, fmt.Sprintf(`{"id":%q,"thread_name":"Task without rollout metadata"}`, fixtureParent))
	reader := LocalReader{AppServerSocket: filepath.Join(home, "missing.sock"), CodexHome: home}

	got, err := reader.Lookup(t.Context(), []string{fixtureParent})
	if err != nil {
		t.Fatal(err)
	}
	metadata := got[fixtureParent]
	if metadata.DisplayName != "Task without rollout metadata" || metadata.ParentTaskID != "" ||
		metadata.Relationship != TaskRelationshipUnknown {
		t.Fatalf("metadata = %#v", metadata)
	}
	if len(metadata.Sources) != 1 || metadata.Sources[0] != TaskMetadataFromLocalFiles {
		t.Fatalf("metadata sources = %#v", metadata.Sources)
	}
}

func TestLookupDoesNotGuessRootForSubagentWithoutKnownParent(t *testing.T) {
	home := t.TempDir()
	writeLocalRollout(t, home, fixtureChild, fmt.Sprintf(
		`{"timestamp":"2026-07-14T10:01:00Z","type":"session_meta","payload":{"id":%q,"source":{"subagent":"review"}}}`, fixtureChild,
	))
	reader := LocalReader{AppServerSocket: filepath.Join(home, "missing.sock"), CodexHome: home}

	got, err := reader.Lookup(t.Context(), []string{fixtureChild})
	if err != nil {
		t.Fatal(err)
	}
	if metadata := got[fixtureChild]; metadata.ParentTaskID != "" || metadata.Relationship != TaskRelationshipUnknown {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestLookupDoesNotTreatUnrelatedLocalStateAsRequestedMetadata(t *testing.T) {
	home := t.TempDir()
	writeLocalNames(t, home, fmt.Sprintf(`{"id":%q,"thread_name":"Parent task"}`, fixtureParent))
	writeLocalRollout(t, home, fixtureParent, fmt.Sprintf(
		`{"timestamp":"2026-07-14T10:00:00Z","type":"session_meta","payload":{"id":%q,"source":"vscode"}}`, fixtureParent,
	))
	reader := LocalReader{AppServerSocket: filepath.Join(home, "missing.sock"), CodexHome: home}
	got, err := reader.Lookup(t.Context(), []string{fixtureChild})
	if !errors.Is(err, ErrUnavailable) || len(got) != 0 {
		t.Fatalf("metadata = %#v, error = %v", got, err)
	}
}

func TestDiscoverMetadataFilesHonoursCancellation(t *testing.T) {
	home := t.TempDir()
	writeLocalRollout(t, home, fixtureParent, fmt.Sprintf(
		`{"timestamp":"2026-07-14T10:00:00Z","type":"session_meta","payload":{"id":%q}}`, fixtureParent,
	))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := discoverMetadataFiles(ctx, home, map[string]struct{}{fixtureParent: {}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestLookupReportsOversizedLocalMetadataWhenAppServerIsUnavailable(t *testing.T) {
	home := t.TempDir()
	writeLocalRollout(t, home, fixtureParent, strings.Repeat("x", 2*localMetadataRecordLimit))
	reader := LocalReader{AppServerSocket: filepath.Join(home, "missing.sock"), CodexHome: home}
	if _, err := reader.Lookup(t.Context(), []string{fixtureParent}); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "first rollout record exceeds") {
		t.Fatalf("error = %v", err)
	}
}

func writeLocalNames(t *testing.T, home string, lines ...string) {
	t.Helper()
	data := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeLocalRollout(t *testing.T, home, taskID, data string) {
	t.Helper()
	directory := filepath.Join(home, "sessions", "2026", "07", "14")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "rollout-2026-07-14T10-00-00-"+taskID+".jsonl")
	if err := os.WriteFile(path, []byte(data+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
