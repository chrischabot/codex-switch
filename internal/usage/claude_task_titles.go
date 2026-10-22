package usage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshp123/codex-switch/internal/store"
)

// Claude names come from its own session files. Reading only metadata leaves
// copied fork history out of accounting, which comes from served responses.
func (ledger *UsageLedger) claudeTaskMetadata(responses []store.CompletedResponseRecord) ([]taskMetadata, error) {
	var metadata []taskMetadata
	seen := make(map[string]bool)
	sessions := map[string]map[string]bool{"code": {}, "pi": {}}
	for _, response := range responses {
		id := string(response.TaskID)
		if seen[id] || !strings.HasPrefix(id, "claude:") {
			continue
		}
		seen[id] = true
		client, session, found := strings.Cut(strings.TrimPrefix(id, "claude:"), ":")
		if !found || session == "" || filepath.Base(session) != session {
			continue
		}
		metadata = append(metadata, taskMetadata{TaskID: id, SourceKind: "claude:" + client})
		if sessions[client] != nil {
			sessions[client][session] = true
		}
	}
	pathsByClient := make(map[string]map[string][]string)
	for _, client := range []string{"code", "pi"} {
		if len(sessions[client]) == 0 {
			continue
		}
		var directory string
		var err error
		if client == "code" {
			directory = os.Getenv("CLAUDE_CONFIG_DIR")
			if directory == "" {
				home, homeErr := os.UserHomeDir()
				if homeErr != nil {
					return nil, homeErr
				}
				directory = filepath.Join(home, ".claude")
			}
			directory = filepath.Join(directory, "projects")
		} else {
			directory, err = piSessionDirectory()
			if err != nil {
				return nil, err
			}
		}
		pathsByClient[client], err = discoverTitlePaths(directory, client == "pi", sessions[client])
		if err != nil {
			return nil, err
		}
	}
	for index := range metadata {
		client := strings.TrimPrefix(metadata[index].SourceKind, "claude:")
		_, session, _ := strings.Cut(strings.TrimPrefix(metadata[index].TaskID, "claude:"), ":")
		for _, path := range pathsByClient[client][session] {
			var err error
			if client == "pi" {
				metadata[index].Title, err = ledger.piSessionTitle(path)
			} else {
				metadata[index].Title, err = ledger.claudeCodeSessionTitle(path, session)
			}
			if err != nil {
				return nil, err
			}
		}
	}
	return metadata, nil
}

// A single directory listing replaces a wildcard expansion for every task.
// Nested matches precede flat Pi matches, preserving the old glob order.
func discoverTitlePaths(directory string, pi bool, sessions map[string]bool) (map[string][]string, error) {
	paths := make(map[string][]string)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return paths, nil // filepath.Glob also ignores directory read errors.
	}
	add := func(path, name string) {
		if !strings.HasSuffix(name, ".jsonl") {
			return
		}
		session := strings.TrimSuffix(name, ".jsonl")
		if !pi {
			if sessions[session] {
				paths[session] = append(paths[session], path)
			}
			return
		}
		for {
			_, suffix, found := strings.Cut(session, "_")
			if !found {
				break
			}
			session = suffix
			if sessions[session] {
				paths[session] = append(paths[session], path)
			}
		}
	}
	for _, entry := range entries {
		// Glob follows directory symlinks too.
		subdirectory := filepath.Join(directory, entry.Name())
		if !entry.IsDir() {
			info, statErr := os.Stat(subdirectory)
			if statErr != nil || !info.IsDir() {
				continue
			}
		}
		children, readErr := os.ReadDir(subdirectory)
		if readErr != nil {
			continue
		} // Like filepath.Glob, unreadable directories have no matches.
		for _, child := range children {
			add(filepath.Join(subdirectory, child.Name()), child.Name())
		}
	}
	if pi {
		for _, entry := range entries {
			add(filepath.Join(directory, entry.Name()), entry.Name())
		}
	}
	return paths, nil
}

func (ledger *UsageLedger) claudeCodeSessionTitle(path, sessionID string) (string, error) {
	state, err := ledger.readSessionTitle(path, "claude", func(state *sessionTitleState, line []byte) {
		if !bytes.Contains(line, []byte(`"custom-title"`)) && !bytes.Contains(line, []byte(`"ai-title"`)) && !bytes.Contains(line, []byte(`"summary"`)) && state.FirstPrompt != "" {
			return
		}
		var record struct {
			Type        string `json:"type"`
			SessionID   string `json:"sessionId"`
			CustomTitle string `json:"customTitle"`
			AITitle     string `json:"aiTitle"`
			Summary     string `json:"summary"`
			IsMeta      bool   `json:"isMeta"`
			Message     struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &record) != nil || (record.SessionID != sessionID && record.SessionID != "") {
			return
		}
		switch record.Type {
		case "custom-title":
			state.Name = strings.TrimSpace(record.CustomTitle)
		case "ai-title":
			state.Generated = strings.TrimSpace(record.AITitle)
		case "summary":
			state.Summary = strings.TrimSpace(record.Summary)
		case "user":
			if !record.IsMeta && state.FirstPrompt == "" {
				state.FirstPrompt = claudePromptTitle(record.Message.Content)
			}
		}
	})
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	name := state.Name

	data, err := os.ReadFile(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "custom-title.json"))
	if err == nil {
		var custom struct {
			Title string `json:"customTitle"`
		}
		if json.Unmarshal(data, &custom) == nil && strings.TrimSpace(custom.Title) != "" {
			name = strings.TrimSpace(custom.Title)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	for _, title := range []string{name, state.Generated, state.Summary, state.FirstPrompt} {
		if title != "" {
			return title, nil
		}
	}
	return "", nil
}

// Claude's session picker uses the first prompt when no saved title exists.
func claudePromptTitle(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) != nil {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(content, &blocks) != nil {
			return ""
		}
		for _, block := range blocks {
			if block.Type == "text" {
				text = block.Text
				break
			}
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	characters := []rune(text)
	if len(characters) > 200 {
		text = string(characters[:200])
	}
	return text
}
