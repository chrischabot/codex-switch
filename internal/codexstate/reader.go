package codexstate

import (
	"os"
	"path/filepath"
)

type LocalReader struct {
	CodexHome       string
	CodexExecutable string
}

func DefaultReader() (*LocalReader, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	return &LocalReader{CodexHome: codexHome, CodexExecutable: FindCodexExecutable()}, nil
}

// FindCodexExecutable prefers the client bundled with the desktop app, so
// Codex Switch speaks the same client version as the app it serves.
func FindCodexExecutable() string {
	const desktopBundledCodex = "/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex"
	if _, err := os.Stat(desktopBundledCodex); err == nil {
		return desktopBundledCodex
	}
	return "codex"
}
