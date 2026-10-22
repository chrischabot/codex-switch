package usage

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"os"
	"syscall"
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

// Persist only complete lines, so a writer's unfinished last record is reread
// after its next append. A valid unterminated line still supplies today's title.
func (ledger *UsageLedger) readSessionTitle(path, kind string, consume func(*sessionTitleState, []byte)) (sessionTitleState, error) {
	var state sessionTitleState
	file, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return state, err
	}
	identity := fmt.Sprintf("%d", info.ModTime().UnixNano())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		identity = fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
	}
	var previousIdentity string
	var previousSize, previousModified int64
	err = ledger.sqlDatabase.QueryRow(`SELECT identity,size,modified,offset,name,generated,summary,first_prompt,folder FROM session_titles WHERE path=? AND kind=?`, path, kind).Scan(&previousIdentity, &previousSize, &previousModified, &state.Offset, &state.Name, &state.Generated, &state.Summary, &state.FirstPrompt, &state.Folder)
	if err != nil && err != sql.ErrNoRows {
		return state, err
	}
	reset := err == sql.ErrNoRows || identity != previousIdentity || info.Size() < previousSize || (info.Size() == previousSize && info.ModTime().UnixNano() != previousModified)
	if reset {
		state = sessionTitleState{}
	}
	if !reset && info.Size() == previousSize && info.ModTime().UnixNano() == previousModified && state.Offset == info.Size() {
		return state, nil
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
	return state, nil
}
