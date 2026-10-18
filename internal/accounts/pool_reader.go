package accounts

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// poolReader caches decoded identities, never bearer tokens. Each read still
// observes membership and both files' versions, including other generations'
// atomic replacements. No watcher delay can hide removal or reserve changes.
type poolReader struct {
	mutex     sync.Mutex
	load      func(string) ([]PoolAccount, error)
	directory string
	versions  map[string]os.FileInfo
	pool      []PoolAccount
}

func poolVersions(directory string) (map[string]os.FileInfo, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	versions := make(map[string]os.FileInfo)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "account-") {
			continue
		}
		index, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "account-"))
		if err != nil || index < 1 {
			continue
		}
		for _, name := range []string{"auth.json", reserveFilename} {
			key := filepath.Join(entry.Name(), name)
			info, err := os.Stat(filepath.Join(directory, key))
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			versions[key] = info
		}
	}
	return versions, nil
}

func samePoolVersions(a, b map[string]os.FileInfo) bool {
	if a == nil || b == nil || len(a) != len(b) {
		return false
	}
	for key, old := range a {
		current, exists := b[key]
		if !exists {
			return false
		}
		if old == nil || current == nil {
			if old != nil || current != nil {
				return false
			}
			continue
		}
		if !os.SameFile(old, current) || old.Size() != current.Size() || old.Mode() != current.Mode() || !old.ModTime().Equal(current.ModTime()) {
			return false
		}
	}
	return true
}

func (reader *poolReader) read(directory string) ([]PoolAccount, error) {
	reader.mutex.Lock()
	defer reader.mutex.Unlock()
	load := reader.load
	if load == nil {
		load = ReadPool
	}
	versions, versionErr := poolVersions(directory)
	if versionErr == nil && reader.directory == directory && samePoolVersions(reader.versions, versions) {
		return slices.Clone(reader.pool), nil
	}
	pool, err := load(directory)
	reader.versions = nil
	if err == nil && versionErr == nil {
		after, afterErr := poolVersions(directory)
		// Never associate a read crossing a replacement with its new version.
		if afterErr == nil && samePoolVersions(versions, after) {
			reader.directory = directory
			reader.versions = after
			reader.pool = slices.Clone(pool)
		}
	}
	return pool, err
}
