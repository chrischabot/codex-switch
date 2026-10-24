package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshp123/codex-switch/internal/taskidentity"
)

// localTaskTitle names the tasks Codex's own database cannot: work no task
// owns, and Pi sessions. Codex names its tasks itself; the menu snapshot
// command reads those names from Codex.
func localTaskTitle(taskID string, system bool, metadata taskMetadata) string {
	switch {
	case metadata.Title != "":
		return metadata.Title
	case system:
		return "System"
	case metadata.SourceKind == piSessionSourceKind:
		return "pi"
	case taskidentity.IsPiSession(taskID):
		return "pi"
	}
	return ""
}

// piSessionTitle is the session's latest name when Pi has one, otherwise
// "pi" and the session's project folder. It never shows prompt text or a path.
func (ledger *UsageLedger) piSessionTitle(path string) (string, error) {
	if ledger.piDiscovery.Started {
		if title, known := ledger.piDiscovery.Titles[path]; known {
			return title, nil
		}
	}

	state, err := ledger.readSessionTitle(path, "pi", func(state *sessionTitleState, line []byte) {
		// Keep Pi's existing rule: oversized lines are tool output, never names.
		if len(line) > 64*1024 || !bytes.Contains(line, []byte(`"type":"session`)) {
			return
		}
		var record struct {
			Type string `json:"type"`
			Cwd  string `json:"cwd"`
			Name string `json:"name"`
		}
		if json.Unmarshal(line, &record) != nil {
			return
		}
		switch {
		case record.Type == "session":
			state.Folder = projectFolderName(record.Cwd)
		case record.Type == "session_info" && strings.TrimSpace(record.Name) != "":
			state.Name = strings.TrimSpace(record.Name)
		}
	})
	if err != nil {
		// Pi sessions may disappear while the menu is being composed.
		var pathError *os.PathError
		if !errors.As(err, &pathError) {
			return "", err
		}
	}
	title := piVisibleTitle(state)
	if ledger.piDiscovery.Started {
		ledger.piDiscovery.Titles[path] = title
	}
	return title, nil
}

func piVisibleTitle(state sessionTitleState) string {
	switch {
	case state.Name != "":
		return state.Name
	case state.Folder != "":
		return "pi · " + state.Folder
	}
	return "pi"
}

func projectFolderName(directory string) string {
	name := filepath.Base(filepath.Clean(directory))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}
