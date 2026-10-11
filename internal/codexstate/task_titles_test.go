package codexstate

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTaskTitlesUseNativeTitleWithoutReplacingExplicitName(t *testing.T) {
	home := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(home, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, name TEXT, title TEXT NOT NULL);
		INSERT INTO threads VALUES
		('unnamed', NULL, 'Check export flow'),
		('renamed', 'Customer exports', 'Check export flow'),
		('blank-name', ' ', 'Check account routing'),
		('empty', NULL, '');`); err != nil {
		t.Fatal(err)
	}
	reader := LocalReader{CodexHome: home}
	titles, err := reader.TaskTitles(t.Context(), []string{"unnamed", "renamed", "blank-name", "empty"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"unnamed": "Check export flow", "renamed": "Customer exports", "blank-name": "Check account routing"}
	if !reflect.DeepEqual(titles, want) {
		t.Fatalf("task titles = %v; want %v", titles, want)
	}
}
