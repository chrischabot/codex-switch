package usage

import (
	"context"
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joshp123/codex-switch/internal/taskidentity"
)

type piSessionDiscovery struct {
	Directory      string
	HighWaterRowID int64
	LastWalk       time.Time
	Started        bool
	WalkOnly       bool
	TaskPaths      map[string][]string
	Pending        map[string]map[string]piPendingResponse
	TitlePaths     map[string][]string
	Titles         map[string]string
}

type piPendingResponse struct {
	RecordedAt  time.Time
	SessionRead bool
}

func clonePiPending(pending map[string]map[string]piPendingResponse) map[string]map[string]piPendingResponse {
	if len(pending) == 0 {
		return nil
	}
	copy := make(map[string]map[string]piPendingResponse, len(pending))
	for task, responses := range pending {
		copy[task] = maps.Clone(responses)
	}
	return copy
}

func piNativeTaskID(task string) string {
	if taskidentity.IsPiSession(task) {
		return task
	}
	if session, found := strings.CutPrefix(task, "claude:pi:"); found && session != "" {
		return taskidentity.PiSession(session)
	}
	return ""
}

func (ledger *UsageLedger) readPiResponseChanges(ctx context.Context) error {
	state := &ledger.piDiscovery
	rows, err := ledger.sqlDatabase.QueryContext(ctx, `SELECT rowid,codex_task_id,completed_response_id,completed_at_unix_nano FROM completed_responses WHERE rowid>? ORDER BY rowid`, state.HighWaterRowID)
	if err != nil {
		return err
	}
	defer rows.Close()
	copied := false
	for rows.Next() {
		var row, recordedAt int64
		var task, response string
		if err = rows.Scan(&row, &task, &response, &recordedAt); err != nil {
			return err
		}
		state.HighWaterRowID = row
		if strings.HasPrefix(task, "claude:pi:") {
			response = strings.TrimPrefix(response, "claude:")
		}
		task = piNativeTaskID(task)
		if task == "" {
			continue
		}
		if !copied {
			state.Pending = clonePiPending(state.Pending)
			copied = true
		}
		if state.Pending == nil {
			state.Pending = make(map[string]map[string]piPendingResponse)
		}
		if state.Pending[task] == nil {
			state.Pending[task] = make(map[string]piPendingResponse)
		}
		state.Pending[task][response] = piPendingResponse{RecordedAt: time.Unix(0, recordedAt)}
	}
	return rows.Err()
}

func (ledger *UsageLedger) importPiSessions(ctx context.Context, directory string, since, now time.Time) (err error) {
	previous := ledger.piDiscovery
	if len(previous.Pending) > 0 {
		ledger.piDiscovery.Pending = clonePiPending(previous.Pending)
	}
	defer func() {
		if err != nil {
			ledger.piDiscovery = previous
		}
	}()
	state := &ledger.piDiscovery
	if state.Directory != directory || !state.Started {
		*state = piSessionDiscovery{
			Directory:  directory,
			TaskPaths:  make(map[string][]string),
			TitlePaths: make(map[string][]string),
			Titles:     make(map[string]string),
		}
		if err = ledger.sqlDatabase.QueryRowContext(ctx, `SELECT COALESCE(MAX(rowid),0) FROM completed_responses`).Scan(&state.HighWaterRowID); err != nil {
			return err
		}
	}
	if err = ledger.readPiResponseChanges(ctx); err != nil {
		return err
	}
	reported := make(map[string]bool, len(state.Pending))
	unknown := false
	for task := range state.Pending {
		reported[task] = true
		if len(state.TaskPaths[task]) == 0 {
			unknown = true
		}
	}
	audit := state.Started && !state.WalkOnly && now.Sub(state.LastWalk) >= time.Hour
	walk := !state.Started || state.WalkOnly || audit || unknown
	var sources []sessionFile
	if !walk {
		for task := range state.Pending {
			for _, path := range state.TaskPaths[task] {
				info, statErr := os.Stat(path)
				if os.IsNotExist(statErr) {
					walk = true
					break
				}
				if statErr != nil {
					return statErr
				}
				if info.ModTime().Before(since) {
					continue
				}
				sources = append(sources, sessionFile{Path: path, TaskID: task, Size: info.Size(), ModifiedAt: info.ModTime(), Identity: sessionFileIdentity(info)})
			}
			if walk {
				break
			}
		}
	}
	walkStarted := now
	if walk {
		state.TitlePaths = make(map[string][]string)
		state.Titles = make(map[string]string)
		sources, err = ledger.discoverPiSessions(directory, since)
		if err != nil {
			return err
		}
		// A response completed during the walk can explain a file the walk saw.
		// Keep it pending until its native observation is imported or the
		// notification expires after a session read.
		if err = ledger.readPiResponseChanges(ctx); err != nil {
			return err
		}
		for task := range state.Pending {
			reported[task] = true
		}
		if state.Started && !state.WalkOnly {
			var imported map[string]sessionFile
			imported, err = ledger.readImportedSources(ctx, sources)
			if err != nil {
				return err
			}
			for _, source := range sources {
				old, found := imported[source.Path]
				unchanged := found && old.TaskID == source.TaskID && old.Identity == source.Identity && old.Size == source.Size && old.ModifiedAt.Equal(source.ModifiedAt)
				if !unchanged && !reported[source.TaskID] {
					state.WalkOnly = true
					ledger.logOnChange("pi-session-record-miss", fmt.Sprintf("Pi usage: proxy records missed changed session %s; walking session folders every cycle until restart", source.Path))
				}
			}
		}
		state.TaskPaths = make(map[string][]string)
		for _, source := range sources {
			state.TaskPaths[source.TaskID] = append(state.TaskPaths[source.TaskID], source.Path)
		}
		if !state.Started {
			ledger.logOnChange("pi-session-start", "Pi usage: walking session folders on startup")
		}
		state.LastWalk = now
	} else if len(sources) > 0 {
		state.Titles = maps.Clone(state.Titles)
		for _, source := range sources {
			delete(state.Titles, source.Path)
		}
		sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	}

	readTasks := make(map[string]bool)
	read := func(ctx context.Context, source sessionFile, cursor sessionReadCursor) (sessionReadResult, error) {
		parsed, readErr := ledger.readPiSession(ctx, source, cursor)
		if readErr == nil {
			readTasks[source.TaskID] = true
		}
		return parsed, readErr
	}
	if err = ledger.importSessionFiles(ctx, sources, read); err != nil {
		return err
	}
	for task, responses := range state.Pending {
		for response, pending := range responses {
			// A walk looked everywhere, so it settles responses recorded before
			// it began, including those of Pi runs that keep no session file.
			if readTasks[task] || (walk && !pending.RecordedAt.After(walkStarted)) {
				pending.SessionRead = true
				responses[response] = pending
			}
		}
	}

	if err = ledger.settlePiPendingResponses(ctx, now); err != nil {
		return err
	}
	state.Started = true
	return nil
}

func (ledger *UsageLedger) settlePiPendingResponses(ctx context.Context, now time.Time) error {
	for task, responses := range ledger.piDiscovery.Pending {
		for response, pending := range responses {
			var imported bool
			if err := ledger.sqlDatabase.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM native_usage_observations WHERE task_id=? AND response_id=?)`, task, response).Scan(&imported); err != nil {
				return err
			}
			if imported || (pending.SessionRead && now.Sub(pending.RecordedAt) >= 5*time.Minute) {
				delete(responses, response)
			}
		}
		if len(responses) == 0 {
			delete(ledger.piDiscovery.Pending, task)
		}
	}
	return nil
}
