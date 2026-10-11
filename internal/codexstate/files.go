package codexstate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const localMetadataRecordLimit = 1 << 20

type threadNameRecord struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
}

type localEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type localSessionMeta struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"session_id"`
	AgentNickname string          `json:"agent_nickname"`
	Source        json.RawMessage `json:"source"`
}

type localSubagent struct {
	ThreadSpawn *localThreadSpawn `json:"thread_spawn"`
}

type localThreadSpawn struct {
	ParentThreadID string `json:"parent_thread_id"`
	AgentNickname  string `json:"agent_nickname"`
}

func (r LocalReader) lookupFiles(ctx context.Context, requested []string) (map[string]TaskMetadata, bool, error) {
	if strings.TrimSpace(r.CodexHome) == "" {
		return nil, false, nil
	}
	result := make(map[string]TaskMetadata)
	queue := append([]string(nil), requested...)
	seen := make(map[string]struct{}, len(queue))
	available := false
	var problems []error
	for len(queue) != 0 {
		if err := ctx.Err(); err != nil {
			return result, available, err
		}

		selected := make(map[string]struct{}, len(queue))
		wave := make([]string, 0, len(queue))
		for _, taskID := range queue {
			if _, duplicate := seen[taskID]; duplicate {
				continue
			}
			seen[taskID] = struct{}{}
			selected[taskID] = struct{}{}
			wave = append(wave, taskID)
		}
		queue = queue[:0]
		if len(wave) == 0 {
			continue
		}

		names, namesAvailable, err := readThreadNames(ctx, filepath.Join(r.CodexHome, "session_index.jsonl"), selected)
		if err != nil {
			if ctx.Err() != nil {
				return result, available || namesAvailable, ctx.Err()
			}
			problems = append(problems, err)
		}
		paths, rolloutsAvailable, err := discoverMetadataFiles(ctx, r.CodexHome, selected)
		available = available || namesAvailable || rolloutsAvailable
		if err != nil {
			if ctx.Err() != nil {
				return result, available, ctx.Err()
			}
			problems = append(problems, err)
		}

		for _, taskID := range wave {
			metadata := TaskMetadata{
				TaskID: taskID, DisplayName: strings.TrimSpace(names[taskID]),
				Relationship: TaskRelationshipUnknown,
				Sources:      []TaskMetadataSource{TaskMetadataFromLocalFiles},
			}
			fileRead := false
			path, err := chooseMetadataFile(paths[taskID])
			if err != nil {
				problems = append(problems, fmt.Errorf("task %s: %w", taskID, err))
			} else if path != "" {
				fromFile, err := readLocalTaskMetadata(path, taskID)
				if err != nil {
					problems = append(problems, fmt.Errorf("task %s: %w", taskID, err))
				} else {
					fileRead = true
					metadata.AgentNickname = fromFile.AgentNickname
					metadata.ParentTaskID = fromFile.ParentTaskID
					metadata.Relationship = fromFile.Relationship
					if metadata.DisplayName == "" {
						metadata.DisplayName = fromFile.DisplayName
					}
				}
			}
			if metadata.DisplayName != "" || metadata.AgentNickname != "" || metadata.ParentTaskID != "" || fileRead {
				result[taskID] = metadata
			}
			if metadata.Relationship == TaskRelationshipKnownParent && metadata.ParentTaskID != "" {
				queue = append(queue, metadata.ParentTaskID)
			}
		}
	}
	return result, available, errors.Join(problems...)
}

func readThreadNames(ctx context.Context, path string, selected map[string]struct{}) (map[string]string, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	defer file.Close()
	names := make(map[string]string)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 32<<10), localMetadataRecordLimit)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return names, true, err
		}
		var record threadNameRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if taskID := canonicalTaskID(record.ID); taskID != "" {
			if _, wanted := selected[taskID]; !wanted {
				continue
			}
			names[taskID] = strings.TrimSpace(record.ThreadName)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, true, fmt.Errorf("read task-name index: %w", err)
	}
	return names, true, nil
}

func discoverMetadataFiles(ctx context.Context, home string, selected map[string]struct{}) (map[string][]string, bool, error) {
	result := make(map[string][]string)
	available := false
	for _, root := range []string{filepath.Join(home, "sessions"), filepath.Join(home, "archived_sessions")} {
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, available, err
		}
		available = true
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if taskID := taskIDFromRolloutName(entry.Name()); taskID != "" {
				if _, wanted := selected[taskID]; wanted {
					result[taskID] = append(result[taskID], path)
				}
			}
			return nil
		})
		if err != nil {
			return result, available, err
		}
	}
	return result, available, nil
}

func taskIDFromRolloutName(name string) string {
	if !strings.HasSuffix(name, ".jsonl") {
		return ""
	}
	name = strings.TrimSuffix(name, ".jsonl")
	if len(name) < 36 {
		return ""
	}
	return canonicalTaskID(name[len(name)-36:])
}

func chooseMetadataFile(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	sort.Strings(paths)
	first, err := os.Stat(paths[0])
	if err != nil {
		return "", err
	}
	for _, path := range paths[1:] {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if !os.SameFile(first, info) {
			return "", errors.New("multiple rollout files claim this task")
		}
	}
	return paths[0], nil
}

func readLocalTaskMetadata(path, taskID string) (TaskMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return TaskMetadata{}, err
	}
	defer file.Close()
	line, err := readFirstMetadataRecord(bufio.NewReaderSize(file, 32<<10))
	if err != nil {
		return TaskMetadata{}, err
	}
	var record localEnvelope
	if err := json.Unmarshal(line, &record); err != nil {
		return TaskMetadata{}, fmt.Errorf("invalid first rollout record: %w", err)
	}
	if record.Type != "session_meta" {
		return TaskMetadata{}, fmt.Errorf("first rollout record is %q, not session_meta", record.Type)
	}
	var meta localSessionMeta
	if err := json.Unmarshal(record.Payload, &meta); err != nil {
		return TaskMetadata{}, fmt.Errorf("invalid session metadata: %w", err)
	}
	owner := canonicalTaskID(meta.ID)
	if owner == "" {
		owner = canonicalTaskID(meta.SessionID)
	}
	if owner != taskID {
		return TaskMetadata{}, errors.New("session metadata identifies another task")
	}
	result := TaskMetadata{
		TaskID: taskID, AgentNickname: strings.TrimSpace(meta.AgentNickname),
		Relationship: TaskRelationshipUnknown,
		Sources:      []TaskMetadataSource{TaskMetadataFromLocalFiles},
	}
	var nickname string
	result.Relationship, result.ParentTaskID, nickname = localTaskRelationship(meta.Source)
	if result.AgentNickname == "" {
		result.AgentNickname = nickname
	}
	return result, nil
}

func localTaskRelationship(source json.RawMessage) (TaskRelationshipState, string, string) {
	var name string
	if json.Unmarshal(source, &name) == nil {
		switch name {
		case "cli", "vscode", "exec", "mcp":
			return TaskRelationshipKnownRoot, "", ""
		default:
			return TaskRelationshipUnknown, "", ""
		}
	}

	var tagged map[string]json.RawMessage
	if json.Unmarshal(source, &tagged) != nil {
		return TaskRelationshipUnknown, "", ""
	}
	if custom, ok := tagged["custom"]; ok {
		var value string
		if json.Unmarshal(custom, &value) == nil && strings.TrimSpace(value) != "" {
			return TaskRelationshipKnownRoot, "", ""
		}
		return TaskRelationshipUnknown, "", ""
	}

	for _, key := range []string{"subagent", "subAgent"} {
		encoded, ok := tagged[key]
		if !ok {
			continue
		}
		var subagent localSubagent
		if json.Unmarshal(encoded, &subagent) != nil || subagent.ThreadSpawn == nil {
			return TaskRelationshipUnknown, "", ""
		}
		spawn := subagent.ThreadSpawn
		parentID := canonicalTaskID(spawn.ParentThreadID)
		if parentID == "" {
			return TaskRelationshipUnknown, "", strings.TrimSpace(spawn.AgentNickname)
		}
		return TaskRelationshipKnownParent, parentID, strings.TrimSpace(spawn.AgentNickname)
	}
	return TaskRelationshipUnknown, "", ""
}

func readFirstMetadataRecord(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 32<<10)
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > localMetadataRecordLimit {
			return nil, fmt.Errorf("first rollout record exceeds %d bytes", localMetadataRecordLimit)
		}
		line = append(line, fragment...)
		switch {
		case err == nil:
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) != 0:
			return line, nil
		case errors.Is(err, io.EOF):
			return nil, errors.New("rollout is empty")
		default:
			return nil, err
		}
	}
}
