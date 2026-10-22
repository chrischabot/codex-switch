package usage

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joshp123/codex-switch/internal/codexstate"
)

type codexSessionDiscovery struct {
	Home         string
	DatabasePath string
	HighWaterMS  int64
	LastWalk     time.Time
	Started      bool
	WalkOnly     bool
}

// The one-second overlap covers Codex versions which round index updates to
// whole seconds. File fingerprints and the existing byte cursors avoid rereads.
func (ledger *UsageLedger) discoverCodexSessionChanges(ctx context.Context, home string, since, now time.Time) ([]sessionFile, error) {
	state := &ledger.codexDiscovery
	if state.Home != home {
		*state = codexSessionDiscovery{Home: home, HighWaterMS: since.UnixMilli()}
	}
	if state.WalkOnly {
		return discoverCodexSessions(home, since, nil)
	}
	reader := codexstate.LocalReader{CodexHome: home}
	databasePath, threads, err := reader.UpdatedThreadRollouts(ctx, state.HighWaterMS-1000)
	if err == nil && state.Started && databasePath != state.DatabasePath {
		// A replacement index has its own watermark, including when restored
		// from an older database.
		state.HighWaterMS = since.UnixMilli()
		databasePath, threads, err = reader.UpdatedThreadRollouts(ctx, state.HighWaterMS-1000)
	}
	var indexed []sessionFile
	first := !state.Started || databasePath != state.DatabasePath
	if err == nil && !first {
		indexed, err = indexedCodexSessionFiles(threads, since)
	}
	if err != nil {
		ledger.logOnChange("codex-session-index", fmt.Sprintf("Codex usage: walking session folders because %v", err))
		return discoverCodexSessions(home, since, nil)
	}
	ledger.logOnChange("codex-session-index", "")
	if first || now.Sub(state.LastWalk) >= time.Hour {
		walked, err := discoverCodexSessions(home, since, nil)
		if err != nil {
			return nil, err
		}
		if first {
			ledger.logOnChange("codex-session-start", "Codex usage: walking session folders on startup or thread index replacement")
		} else {
			// Query again after the walk so a thread updated during the walk is
			// not mistaken for a missed index update. Keep the earlier watermark
			// so changes after the walked file's stat are imported next cycle.
			_, checkedThreads, err := reader.UpdatedThreadRollouts(ctx, state.HighWaterMS-1000)
			if err != nil {
				ledger.logOnChange("codex-session-index", fmt.Sprintf("Codex usage: walking session folders because %v", err))
				return walked, nil
			}
			previous, err := ledger.readImportedSources(ctx, walked)
			if err != nil {
				return nil, err
			}
			reported := make(map[string]bool, len(checkedThreads))
			for _, thread := range checkedThreads {
				reported[thread.Path] = true
			}
			for _, source := range walked {
				imported, found := previous[source.Path]
				unchanged := found && imported.TaskID == source.TaskID && imported.Identity == source.Identity && imported.Size == source.Size && imported.ModifiedAt.Equal(source.ModifiedAt)
				if !unchanged && !reported[source.Path] {
					state.WalkOnly = true
					ledger.logOnChange("codex-session-index-miss", fmt.Sprintf("Codex usage: thread index missed changed rollout %s; walking session folders every cycle until restart", source.Path))
				}
			}
		}
		state.LastWalk = now
		indexed = walked
	}
	state.Started, state.DatabasePath = true, databasePath
	for _, thread := range threads {
		state.HighWaterMS = max(state.HighWaterMS, thread.UpdatedAtMS)
	}
	return indexed, nil
}

func indexedCodexSessionFiles(threads []codexstate.ThreadRollout, since time.Time) ([]sessionFile, error) {
	var sources []sessionFile
	for _, thread := range threads {
		// The walk imports only .jsonl files. Compressed archives remain
		// excluded exactly as before.
		if !strings.HasSuffix(thread.Path, ".jsonl") {
			continue
		}
		info, err := os.Stat(thread.Path)
		if os.IsNotExist(err) && thread.Archived {
			if _, compressedErr := os.Stat(thread.Path + ".zst"); compressedErr == nil {
				continue
			}
		}
		if err != nil {
			return nil, fmt.Errorf("stat indexed rollout: %w", err)
		}
		if info.ModTime().Before(since) {
			continue
		}
		sources = append(sources, sessionFile{Path: thread.Path, TaskID: thread.ID, Identity: sessionFileIdentity(info), Size: info.Size(), ModifiedAt: info.ModTime()})
	}
	return sources, nil
}
