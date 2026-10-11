package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/josh/codex-switch/internal/taskidentity"
)

// localTaskTitle names the tasks Codex's own database cannot: work no task
// owns, and Pi sessions. Codex names its tasks itself; the menu snapshot
// command reads those names from Codex.
func localTaskTitle(taskID string, system bool, metadata taskMetadata) string {
	switch {
	case system:
		return "System"
	case metadata.SourceKind == piSessionSourceKind:
		return piSessionTitle(metadata.SourcePath)
	case taskidentity.IsPiSession(taskID):
		return "pi"
	}
	return ""
}

// piSessionTitle is the session's latest name when Pi has one, otherwise
// "pi" and the session's project folder. It never shows prompt text or a path.
func piSessionTitle(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "pi"
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	var name, folder string
	for {
		line, err := reader.ReadSlice('\n')
		for err == bufio.ErrBufferFull {
			// A line longer than the buffer is tool output, never a name.
			line = nil
			_, err = reader.ReadSlice('\n')
		}
		if bytes.Contains(line, []byte(`"type":"session`)) {
			var record struct {
				Type string `json:"type"`
				Cwd  string `json:"cwd"`
				Name string `json:"name"`
			}
			if json.Unmarshal(line, &record) == nil {
				switch {
				case record.Type == "session":
					folder = projectFolderName(record.Cwd)
				case record.Type == "session_info" && strings.TrimSpace(record.Name) != "":
					name = strings.TrimSpace(record.Name)
				}
			}
		}
		if err != nil {
			break
		}
	}
	switch {
	case name != "":
		return name
	case folder != "":
		return "pi · " + folder
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
