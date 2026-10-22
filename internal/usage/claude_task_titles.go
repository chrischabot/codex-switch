package usage

import (
	"bytes"
	"encoding/json"
	"iter"
	"os"
	"path/filepath"
	"strings"
)

// Claude names come from its own session files. Usage comes from served responses.
func (ledger *UsageLedger) visitClaudeTaskMetadata(taskIDs iter.Seq[string], visit func(taskMetadata)) error {
	var ids []string
	seen := make(map[string]bool)
	sessions := map[string]map[string]bool{"code": {}, "pi": {}}
	for id := range taskIDs {
		if seen[id] || !strings.HasPrefix(id, "claude:") {
			continue
		}
		seen[id] = true
		client, session, found := strings.Cut(strings.TrimPrefix(id, "claude:"), ":")
		if !found || session == "" || filepath.Base(session) != session {
			continue
		}
		ids = append(ids, id)
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
					return homeErr
				}
				directory = filepath.Join(home, ".claude")
			}
			directory = filepath.Join(directory, "projects")
		} else {
			directory, err = piSessionDirectory()
			if err != nil {
				return err
			}
		}
		pathsByClient[client], err = ledger.cachedTitlePaths(directory, client == "pi", sessions[client])
		if err != nil {
			return err
		}
	}
	for _, id := range ids {
		client, session, _ := strings.Cut(strings.TrimPrefix(id, "claude:"), ":")
		item := taskMetadata{TaskID: id, SourceKind: "claude:" + client}
		for _, path := range pathsByClient[client][session] {
			var err error
			if client == "pi" {
				item.Title, err = ledger.piSessionTitle(path)
			} else {
				item.Title, err = ledger.claudeCodeSessionTitle(path, session)
			}
			if err != nil {
				return err
			}
		}
		visit(item)
	}
	return nil
}

// A single directory listing replaces a wildcard expansion for every task.
// Nested matches precede flat Pi matches, preserving the old glob order.
func discoverTitlePathsWithDirectoryReader(directory string, pi bool, sessions map[string]bool, readDirectory func(string) ([]os.DirEntry, error)) (map[string][]string, error) {
	paths := make(map[string][]string)
	entries, err := readDirectory(directory)
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
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			info, statErr := os.Stat(subdirectory)
			if statErr != nil || !info.IsDir() {
				continue
			}
		}
		children, readErr := readDirectory(subdirectory)
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

	override, err := ledger.cachedClaudeCustomTitle(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "custom-title.json"))
	if err != nil {
		return "", err
	}
	if override != "" {
		name = override
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

func (ledger *UsageLedger) cachedClaudeCustomTitle(path string) (string, error) {
	cache := &ledger.sessionFiles
	cache.Lock()
	defer cache.Unlock()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		delete(cache.CustomTitles, path)
		return "", nil
	}
	if err != nil {
		return "", err
	}
	fingerprint := fingerprintSessionFile(info)
	if previous, exists := cache.CustomTitles[path]; exists && previous.Fingerprint == fingerprint {
		return previous.Name, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var custom struct {
		Title string `json:"customTitle"`
	}
	var name string
	if json.Unmarshal(data, &custom) == nil {
		name = strings.TrimSpace(custom.Title)
	}
	if cache.CustomTitles == nil {
		cache.CustomTitles = make(map[string]cachedCustomTitle)
	}
	cache.CustomTitles[path] = cachedCustomTitle{Fingerprint: fingerprint, Name: name}
	return name, nil
}

// Stat cached directories before using known paths: a new duplicate session
// file can change glob-order title precedence. A directory change makes its
// path sets unknown; only changed directories are listed again. Child changes
// matter independently of the projects directory.
func (ledger *UsageLedger) cachedTitlePaths(directory string, pi bool, sessions map[string]bool) (map[string][]string, error) {
	cache := &ledger.sessionFiles
	cache.Lock()
	defer cache.Unlock()
	key := directory
	if pi {
		key = "pi\x00" + directory
	}
	if cache.TitlePaths == nil {
		cache.TitlePaths = make(map[string]map[string][]string)
	}
	known := cache.TitlePaths[key]
	for path, previous := range cache.Directories {
		if path != directory && filepath.Dir(path) != directory {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || fingerprintSessionFile(info) != previous.Fingerprint {
			known = nil
			if err != nil {
				delete(cache.Directories, path)
			}
		}
	}
	if known == nil {
		known = make(map[string][]string)
		cache.TitlePaths[key] = known
	}
	paths := make(map[string][]string, len(sessions))
	missing := make(map[string]bool)
	for session := range sessions {
		previous := known[session]
		if len(previous) != 0 {
			paths[session] = previous
		} else {
			missing[session] = true
		}
	}
	if len(missing) == 0 {
		return paths, nil
	}
	found, err := discoverTitlePathsWithDirectoryReader(directory, pi, missing, func(path string) ([]os.DirEntry, error) {
		info, err := os.Stat(path)
		if err != nil {
			delete(cache.Directories, path)
			return nil, err
		}
		fingerprint := fingerprintSessionFile(info)
		if previous, exists := cache.Directories[path]; exists && previous.Fingerprint == fingerprint {
			return previous.Entries, nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		if cache.Directories == nil {
			cache.Directories = make(map[string]cachedTitleDirectory)
		}
		cache.Directories[path] = cachedTitleDirectory{Fingerprint: fingerprint, Entries: entries}
		return entries, nil
	})
	if err != nil {
		return nil, err
	}
	for session := range missing {
		known[session] = found[session]
		paths[session] = found[session]
	}
	return paths, nil
}
