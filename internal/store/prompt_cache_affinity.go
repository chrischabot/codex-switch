package store

import (
	"database/sql"
	"errors"
	"time"
)

// ReadPromptCacheParent looks up a matching reusable prefix only for an
// unpinned session. The index expires with Anthropic's longest cache lifetime.
func (database *StoreDatabase) ReadPromptCacheParent(taskID TaskID, prefixes []string) (TaskID, error) {
	if _, pinned, err := database.ReadTaskAccountPin(taskID); err != nil || pinned {
		return "", err
	}
	for index := len(prefixes) - 2; index >= 0; index-- {
		var parent TaskID
		err := database.sqlDatabase.QueryRow(`SELECT task_id FROM prompt_cache_affinity WHERE prefix_hash = ? AND seen_at_unix_nano > ?`, prefixes[index], time.Now().Add(-time.Hour).UnixNano()).Scan(&parent)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", err
		}
		if parent != taskID {
			return parent, nil
		}
	}
	return "", nil
}

func (database *StoreDatabase) RememberPromptCachePrefix(taskID TaskID, prefix string) error {
	if taskID == "" || prefix == "" {
		return nil
	}
	_, err := database.sqlDatabase.Exec(`INSERT INTO prompt_cache_affinity(prefix_hash,task_id,seen_at_unix_nano) VALUES(?,?,?) ON CONFLICT(prefix_hash) DO UPDATE SET task_id=excluded.task_id,seen_at_unix_nano=excluded.seen_at_unix_nano`, prefix, taskID, time.Now().UnixNano())
	if err != nil {
		return err
	}
	_, err = database.sqlDatabase.Exec(`DELETE FROM prompt_cache_affinity WHERE seen_at_unix_nano < ?`, time.Now().Add(-time.Hour).UnixNano())
	return err
}
