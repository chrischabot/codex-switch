package accounts

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// codexClientVersion reports the selected Codex executable's version, so an app
// or CLI upgrade needs no Switch update. It runs `codex --version` again only
// when the executable file changes: account refreshes ask every minute, and
// every child process the proxy starts costs a fork.
func codexClientVersion(ctx context.Context, executable string) (string, error) {
	info, err := os.Stat(executable)
	if err != nil {
		resolved, lookErr := exec.LookPath(executable)
		if lookErr != nil {
			return "", fmt.Errorf("find Codex executable: %w", err)
		}
		if info, err = os.Stat(resolved); err != nil {
			return "", fmt.Errorf("find Codex executable: %w", err)
		}
	}
	executableIdentity := codexExecutableIdentity{path: executable, size: info.Size(), modified: info.ModTime()}
	cachedCodexClientVersion.mutex.Lock()
	defer cachedCodexClientVersion.mutex.Unlock()
	if cachedCodexClientVersion.version != "" && cachedCodexClientVersion.executable == executableIdentity {
		return cachedCodexClientVersion.version, nil
	}
	output, err := exec.CommandContext(ctx, executable, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("read Codex executable version: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 || fields[0] != "codex-cli" {
		return "", fmt.Errorf("Codex executable did not report a codex-cli version")
	}
	cachedCodexClientVersion.executable, cachedCodexClientVersion.version = executableIdentity, fields[1]
	return fields[1], nil
}

type codexExecutableIdentity struct {
	path     string
	size     int64
	modified time.Time
}

var cachedCodexClientVersion struct {
	mutex      sync.Mutex
	executable codexExecutableIdentity
	version    string
}
