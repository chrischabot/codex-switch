package codexstate

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// TaskTitles reads only requested titles from the current Codex home, without
// importing rollouts or reading Codex Switch's accounting database.
func (r LocalReader) TaskTitles(ctx context.Context, taskIDs []string) (map[string]string, error) {
	result := map[string]string{}
	if len(taskIDs) == 0 {
		return result, nil
	}
	paths, err := filepath.Glob(filepath.Join(r.CodexHome, "state_*.sqlite"))
	if err != nil {
		return nil, err
	}
	var newest os.FileInfo
	var databasePath string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if newest == nil || info.ModTime().After(newest.ModTime()) {
			newest = info
			databasePath = path
		}
	}
	if databasePath == "" {
		return result, nil
	}
	databaseURL := url.URL{Scheme: "file", Path: databasePath, RawQuery: "mode=ro"}
	database, err := sql.Open("sqlite", databaseURL.String())
	if err != nil {
		return nil, err
	}
	defer database.Close()
	parameters := make([]any, len(taskIDs))
	for index, id := range taskIDs {
		parameters[index] = id
	}
	rows, err := database.QueryContext(ctx, "SELECT id,COALESCE(NULLIF(TRIM(name),''),title) FROM threads WHERE id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(taskIDs)), ",")+")", parameters...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		if strings.TrimSpace(title) != "" {
			result[id] = title
		}
	}
	return result, rows.Err()
}
