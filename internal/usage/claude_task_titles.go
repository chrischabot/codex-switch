package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshp123/codex-switch/internal/store"
)

// Claude names come from its own session files. Reading only metadata leaves
// copied fork history out of accounting, which comes from served responses.
func claudeTaskMetadata(responses []store.CompletedResponseRecord) ([]taskMetadata, error) {
	var metadata []taskMetadata
	seen := make(map[string]bool)
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
		var paths []string
		var err error
		switch client {
		case "code":
			directory := os.Getenv("CLAUDE_CONFIG_DIR")
			if directory == "" {
				home, homeErr := os.UserHomeDir()
				if homeErr != nil {
					return nil, homeErr
				}
				directory = filepath.Join(home, ".claude")
			}
			paths, err = filepath.Glob(filepath.Join(directory, "projects", "*", session+".jsonl"))
		case "pi":
			directory, directoryErr := piSessionDirectory()
			if directoryErr != nil {
				return nil, directoryErr
			}
			paths, err = filepath.Glob(filepath.Join(directory, "*", "*_"+session+".jsonl"))
			flatPaths, flatErr := filepath.Glob(filepath.Join(directory, "*_"+session+".jsonl"))
			if flatErr != nil {
				return nil, flatErr
			}
			paths = append(paths, flatPaths...)
		}
		if err != nil {
			return nil, err
		}
		item := taskMetadata{TaskID: id, SourceKind: "claude:" + client}
		for _, path := range paths {
			if client == "pi" {
				item.Title = piSessionTitle(path)
			} else {
				item.Title, err = claudeCodeSessionTitle(path, session)
				if err != nil {
					return nil, err
				}
			}
		}
		metadata = append(metadata, item)
	}
	return metadata, nil
}

func claudeCodeSessionTitle(path, sessionID string) (string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	var name, generated, summary, firstPrompt string
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"custom-title"`)) || bytes.Contains(line, []byte(`"ai-title"`)) || bytes.Contains(line, []byte(`"summary"`)) || firstPrompt == "" {
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
			if json.Unmarshal(line, &record) == nil && (record.SessionID == sessionID || record.SessionID == "") {
				switch record.Type {
				case "custom-title":
					name = strings.TrimSpace(record.CustomTitle)
				case "ai-title":
					generated = strings.TrimSpace(record.AITitle)
				case "summary":
					summary = strings.TrimSpace(record.Summary)
				case "user":
					if !record.IsMeta && firstPrompt == "" {
						firstPrompt = claudePromptTitle(record.Message.Content)
					}
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
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
	for _, title := range []string{name, generated, summary, firstPrompt} {
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
