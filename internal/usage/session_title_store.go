package usage

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

type sessionTitleState struct {
	Offset      int64
	Name        string
	Generated   string
	Summary     string
	FirstPrompt string
	Folder      string
}

func createSessionTitleSchema(database *sql.DB) error {
	_, err := database.Exec(`CREATE TABLE IF NOT EXISTS session_titles (
 path TEXT NOT NULL, kind TEXT NOT NULL, identity TEXT NOT NULL, size INTEGER NOT NULL, modified INTEGER NOT NULL,
 offset INTEGER NOT NULL, name TEXT NOT NULL, generated TEXT NOT NULL, summary TEXT NOT NULL, first_prompt TEXT NOT NULL, folder TEXT NOT NULL,
 PRIMARY KEY(path,kind)
 ) STRICT;`)
	return err
}

type sessionFileNode struct {
	Device           uint64
	Inode            uint64
	ModifiedFallback int64
}

type sessionFileFingerprint struct {
	Identity sessionFileNode
	Size     int64
	Modified time.Time
}

func fingerprintSessionFile(info os.FileInfo) sessionFileFingerprint {
	fingerprint := sessionFileFingerprint{Size: info.Size(), Modified: info.ModTime()}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		fingerprint.Identity = sessionFileNode{Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}
	} else {
		fingerprint.Identity.ModifiedFallback = info.ModTime().UnixNano()
	}
	return fingerprint
}

func sessionFileIdentity(info os.FileInfo) string {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
	}
	return fmt.Sprintf("%d", info.ModTime().UnixNano())
}

type sessionTitleKey struct {
	Path, Kind string
}

type cachedSessionTitle struct {
	Fingerprint sessionFileFingerprint
	Visible     sessionTitleState
}

type cachedPiHeader struct {
	Fingerprint sessionFileFingerprint
	Header      piSessionHeader
	Err         error
}

type cachedCustomTitle struct {
	Fingerprint sessionFileFingerprint
	Name        string
}

type cachedTitleDirectory struct {
	Fingerprint sessionFileFingerprint
	Entries     []os.DirEntry
}

// Keep parsed metadata only; session contents remain on disk.
type sessionFileCache struct {
	sync.Mutex
	Titles         map[sessionTitleKey]cachedSessionTitle
	Headers        map[string]cachedPiHeader
	CustomTitles   map[string]cachedCustomTitle
	Directories    map[string]cachedTitleDirectory
	TitlePaths     map[string]map[string][]string
	ClaudeRegistry map[string]cachedClaudeRegistryEntry
	ClaudeTitles   map[string]cachedClaudeTaskTitle
}

func (ledger *UsageLedger) cachedPiSessionHeader(path string) (piSessionHeader, error) {
	cache := &ledger.sessionFiles
	cache.Lock()
	defer cache.Unlock()
	info, err := os.Stat(path)
	if err != nil {
		return piSessionHeader{}, err
	}
	fingerprint := fingerprintSessionFile(info)
	if previous, exists := cache.Headers[path]; exists && previous.Fingerprint == fingerprint {
		return previous.Header, previous.Err
	}
	header, err := readPiSessionHeader(path)
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, errNotPiSession) {
		if cache.Headers == nil {
			cache.Headers = make(map[string]cachedPiHeader)
		}
		cache.Headers[path] = cachedPiHeader{Fingerprint: fingerprint, Header: header, Err: err}
	}
	return header, err
}

// Persist only complete lines, so a writer's unfinished last record is reread
// after its next append. Cache only the visible title; the complete-line cursor
// stays in SQLite, so an unfinished last line cannot advance the next import.
func (ledger *UsageLedger) readSessionTitle(path, kind string, consume func(*sessionTitleState, []byte)) (sessionTitleState, error) {
	cache := &ledger.sessionFiles
	cache.Lock()
	defer cache.Unlock()
	var state sessionTitleState
	info, err := os.Stat(path)
	if err != nil {
		return state, err
	}
	fingerprint := fingerprintSessionFile(info)
	key := sessionTitleKey{Path: path, Kind: kind}
	previous, cached := cache.Titles[key]
	if cached && previous.Fingerprint == fingerprint {
		return previous.Visible, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return state, err
	}
	fingerprint = fingerprintSessionFile(info)
	identity := sessionFileIdentity(info)
	var previousFingerprint sessionFileFingerprint
	var previousModified int64
	var previousIdentity string
	err = ledger.sqlDatabase.QueryRow(`SELECT identity,size,modified,offset,name,generated,summary,first_prompt,folder FROM session_titles WHERE path=? AND kind=?`, path, kind).Scan(&previousIdentity, &previousFingerprint.Size, &previousModified, &state.Offset, &state.Name, &state.Generated, &state.Summary, &state.FirstPrompt, &state.Folder)
	if err != nil && err != sql.ErrNoRows {
		return state, err
	}
	previousFingerprint.Modified = time.Unix(0, previousModified)
	identityChanged := identity != previousIdentity
	reset := err == sql.ErrNoRows || identityChanged || fingerprint.Size < previousFingerprint.Size || (fingerprint.Size == previousFingerprint.Size && !fingerprint.Modified.Equal(previousFingerprint.Modified))
	if reset {
		state = sessionTitleState{}
	}

	if _, err = file.Seek(state.Offset, io.SeekStart); err != nil {
		return state, err
	}
	reader := bufio.NewReaderSize(io.LimitReader(file, info.Size()-state.Offset), 64*1024)
	var tail []byte
	for {
		line, readErr := reader.ReadSlice('\n')
		lineBytes := int64(len(line))
		if readErr == bufio.ErrBufferFull {
			if kind != "pi" {
				tail = append(tail[:0], line...)
			}
			for readErr == bufio.ErrBufferFull {
				line, readErr = reader.ReadSlice('\n')
				lineBytes += int64(len(line))
				if kind != "pi" {
					tail = append(tail, line...)
				}
			}
			line = tail
		}
		if readErr == io.EOF {
			tail = append([]byte(nil), line...)
			break
		}
		if readErr != nil {
			return state, readErr
		}
		consume(&state, line)
		state.Offset += lineBytes
		tail = nil
	}

	_, err = ledger.sqlDatabase.Exec(`INSERT INTO session_titles VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(path,kind) DO UPDATE SET identity=excluded.identity,size=excluded.size,modified=excluded.modified,offset=excluded.offset,name=excluded.name,generated=excluded.generated,summary=excluded.summary,first_prompt=excluded.first_prompt,folder=excluded.folder`, path, kind, identity, info.Size(), info.ModTime().UnixNano(), state.Offset, state.Name, state.Generated, state.Summary, state.FirstPrompt, state.Folder)
	if err != nil {
		return state, err
	}
	consume(&state, tail)
	if cache.Titles == nil {
		cache.Titles = make(map[sessionTitleKey]cachedSessionTitle)
	}
	cache.Titles[key] = cachedSessionTitle{Fingerprint: fingerprint, Visible: state}
	return state, nil
}
