package codexusage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"
)

func Discover(home string, since time.Time) ([]SourceFile, error) {
	byTask := map[string]SourceFile{}
	for _, directory := range []string{"archived_sessions", "sessions"} {
		err := filepath.WalkDir(filepath.Join(home, directory), func(path string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.ModTime().Before(since) {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			first, readErr := bufio.NewReader(file).ReadBytes('\n')
			file.Close()
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
			var metadata record
			if err = json.Unmarshal(first, &metadata); err != nil {
				return fmt.Errorf("parse metadata %s: %w", path, err)
			}
			if metadata.Type != "session_meta" || metadata.Payload.ID == "" {
				return fmt.Errorf("missing session metadata: %s", path)
			}
			taskID := metadata.Payload.ID

			identity := fmt.Sprintf("%v", info.ModTime().UnixNano())
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				identity = fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
			}
			byTask[taskID] = SourceFile{Path: path, TaskID: taskID, Size: info.Size(), ModifiedAt: info.ModTime(), Identity: identity}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	result := make([]SourceFile, 0, len(byTask))
	for _, source := range byTask {
		result = append(result, source)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

type usageRecord struct {
	Input      int64  `json:"input_tokens"`
	Cached     int64  `json:"cached_input_tokens"`
	CacheWrite *int64 `json:"cache_write_input_tokens"`
	Output     int64  `json:"output_tokens"`
	Reasoning  int64  `json:"reasoning_output_tokens"`
	Total      int64  `json:"total_tokens"`
}

func (u *usageRecord) usage() *TokenUsage {
	if u == nil {
		return nil
	}
	return &TokenUsage{u.Input, u.Cached, u.CacheWrite, u.Output, u.Reasoning, u.Total}
}

type record struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Ordinal   int64     `json:"ordinal"`
	Payload   struct {
		ThreadSettings *struct {
			Model string  `json:"model"`
			Tier  *string `json:"service_tier"`
		} `json:"thread_settings"`
		ResponseID     string          `json:"response_id"`
		RootTurnID     string          `json:"root_turn_id"`
		SessionID      string          `json:"session_id"`
		Usage          *usageRecord    `json:"usage"`
		ThreadUsage    *usageRecord    `json:"thread_token_usage"`
		ID             string          `json:"id"`
		Timestamp      time.Time       `json:"timestamp"`
		ParentThreadID string          `json:"parent_thread_id"`
		ForkedFrom     string          `json:"forked_from_id"`
		Source         json.RawMessage `json:"source"`
		HistoryOrdinal *int64          `json:"subagent_history_start_ordinal"`
		Type           string          `json:"type"`
		Model          string          `json:"model"`
		Tier           *string         `json:"service_tier"`
		ThreadID       string          `json:"thread_id"`
		TurnID         string          `json:"turn_id"`
		StartedAt      *int64          `json:"started_at"`
		Info           *struct {
			Total *usageRecord `json:"total_token_usage"`
			Last  *usageRecord `json:"last_token_usage"`
		} `json:"info"`
		RateLimits *struct {
			LimitID   string        `json:"limit_id"`
			PlanType  string        `json:"plan_type"`
			Primary   *windowRecord `json:"primary"`
			Secondary *windowRecord `json:"secondary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}
type windowRecord struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

func (w *windowRecord) window() *AllowanceWindow {
	if w == nil {
		return nil
	}
	return &AllowanceWindow{w.UsedPercent, w.WindowMinutes, time.Unix(w.ResetsAt, 0)}
}
func relevant(prefix []byte) bool {
	for _, marker := range [][]byte{[]byte(`"session_meta"`), []byte(`"turn_context"`), []byte(`"token_count"`), []byte(`"token_usage_record"`), []byte(`"task_started"`), []byte(`"thread_settings_applied"`)} {
		if bytes.Contains(prefix, marker) {
			return true
		}
	}
	return false
}

// Read parses only complete records within the discovered size. Large tool-output lines are discarded without allocation.
func Read(ctx context.Context, source SourceFile, cursor Cursor) (ReadResult, error) {
	result := ReadResult{Cursor: cursor}
	file, err := os.Open(source.Path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	first := bufio.NewReader(file)
	line, err := first.ReadBytes('\n')
	if err != nil {
		return result, fmt.Errorf("read session metadata %s: %w", source.Path, err)
	}
	var metadata record
	if err = json.Unmarshal(line, &metadata); err != nil {
		return result, err
	}
	if metadata.Type != "session_meta" || metadata.Payload.ID != source.TaskID {
		return result, fmt.Errorf("invalid session metadata: %s", source.Path)
	}
	created := metadata.Payload.Timestamp
	if created.IsZero() {
		created = metadata.Timestamp
	}
	result.Metadata = TaskMetadata{TaskID: source.TaskID, ForkedFromTaskID: metadata.Payload.ForkedFrom, CreatedAt: created}
	var origin struct {
		Subagent struct {
			ThreadSpawn struct {
				ParentThreadID string `json:"parent_thread_id"`
			} `json:"thread_spawn"`
		} `json:"subagent"`
	}
	_ = json.Unmarshal(metadata.Payload.Source, &origin)
	result.Metadata.ParentTaskID = metadata.Payload.ParentThreadID
	if result.Metadata.ParentTaskID == "" {
		result.Metadata.ParentTaskID = origin.Subagent.ThreadSpawn.ParentThreadID
	}
	var sourceKind string
	if json.Unmarshal(metadata.Payload.Source, &sourceKind) == nil {
		result.Metadata.SourceKind = sourceKind
	} else {
		var sourceObject struct {
			Subagent json.RawMessage `json:"subagent"`
		}
		_ = json.Unmarshal(metadata.Payload.Source, &sourceObject)
		if json.Unmarshal(sourceObject.Subagent, &sourceKind) == nil {
			result.Metadata.SourceKind = "subagent:" + sourceKind
		} else {
			var subagentKinds map[string]json.RawMessage
			_ = json.Unmarshal(sourceObject.Subagent, &subagentKinds)
			for kind, value := range subagentKinds {
				if kind == "other" {
					var other string
					_ = json.Unmarshal(value, &other)
					result.Metadata.SourceKind = "subagent:" + other
				} else {
					result.Metadata.SourceKind = "subagent:" + kind
				}
			}
		}
	}
	if cursor.Offset == 0 && result.Metadata.ParentTaskID == "" && result.Metadata.ForkedFromTaskID == "" {
		result.Cursor.ChildHistoryEnded = true
	}
	if _, err = file.Seek(cursor.Offset, io.SeekStart); err != nil {
		return result, err
	}
	reader := bufio.NewReaderSize(io.LimitReader(file, source.Size-cursor.Offset), 64*1024)
	offset := cursor.Offset

	for {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		start := offset
		fragment, readErr := reader.ReadSlice('\n')
		if readErr == io.EOF {
			break
		}
		keep := relevant(fragment)
		var complete []byte
		if keep {
			complete = append(complete, fragment...)
		}
		offset += int64(len(fragment))
		for readErr == bufio.ErrBufferFull {
			fragment, readErr = reader.ReadSlice('\n')
			offset += int64(len(fragment))
			if keep {
				complete = append(complete, fragment...)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, readErr
		}
		result.Cursor.Offset = offset
		result.BytesRead = offset - cursor.Offset
		if !keep {
			continue
		}
		var event record
		if err = json.Unmarshal(complete, &event); err != nil {
			return result, fmt.Errorf("parse %s offset %d: %w", source.Path, start, err)
		}
		if !result.Cursor.ChildHistoryEnded {
			if metadata.Payload.HistoryOrdinal != nil {
				result.Cursor.ChildHistoryEnded = event.Ordinal >= *metadata.Payload.HistoryOrdinal
			} else if event.Payload.Type == "task_started" && event.Payload.StartedAt != nil {
				result.Cursor.ChildHistoryEnded = *event.Payload.StartedAt >= created.Unix()
			}
		}
		payload := event.Payload
		if event.Type == "turn_context" {
			result.Cursor.Model = payload.Model
			if payload.TurnID != "" {
				result.Cursor.TurnID = payload.TurnID
			}
		}
		if payload.Type == "task_started" {
			result.Cursor.TurnID = payload.TurnID
		}
		if payload.Type == "thread_settings_applied" && result.Cursor.ChildHistoryEnded && (payload.ThreadID == "" || payload.ThreadID == source.TaskID) {
			if payload.ThreadSettings != nil {
				result.Cursor.Model = payload.ThreadSettings.Model
				result.Cursor.ServiceTier = payload.ThreadSettings.Tier
			}
		}
		if event.Type == "token_usage_record" {
			if payload.ThreadID == source.TaskID && payload.ResponseID != "" && payload.Usage != nil {
				result.Cursor.ChildHistoryEnded = true
				result.Cursor.CanonicalUsageSeen = true
				result.Cursor.PendingResponseID = payload.ResponseID
				result.Cursor.PendingUsage = payload.Usage.usage()
				result.Cursor.PreviousTotals = payload.ThreadUsage.usage()
				result.Observations = append(result.Observations, Observation{SourceKind: result.Metadata.SourceKind, ResponseID: payload.ResponseID, RootTurnID: payload.RootTurnID, SessionID: payload.SessionID, TaskID: payload.ThreadID, TurnID: payload.TurnID, SourcePath: source.Path, SourceOffset: start, At: event.Timestamp, Model: result.Cursor.Model, ServiceTier: result.Cursor.ServiceTier, Usage: payload.Usage.usage()})
			}
			continue
		}
		if payload.Type != "token_count" {
			continue
		}
		var usage *TokenUsage
		allowanceResponseID := ""
		if result.Cursor.PendingResponseID != "" {
			if payload.Info != nil && reflect.DeepEqual(payload.Info.Last.usage(), result.Cursor.PendingUsage) {
				allowanceResponseID = result.Cursor.PendingResponseID
			}
			result.Cursor.PendingResponseID = ""
			result.Cursor.PendingUsage = nil
		}
		if payload.Info != nil {
			total := payload.Info.Total.usage()
			if !reflect.DeepEqual(total, result.Cursor.PreviousTotals) {
				usage = payload.Info.Last.usage()
			}
			result.Cursor.PreviousTotals = total
		}
		if !result.Cursor.ChildHistoryEnded {
			continue
		}
		if result.Cursor.CanonicalUsageSeen {
			usage = nil
		}
		if usage != nil && usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CachedInputTokens == 0 && usage.ReasoningOutputTokens == 0 {
			usage = nil
		}
		observation := Observation{SourceKind: result.Metadata.SourceKind, ResponseID: allowanceResponseID, TaskID: source.TaskID, TurnID: result.Cursor.TurnID, SourcePath: source.Path, SourceOffset: start, At: event.Timestamp, Model: result.Cursor.Model, ServiceTier: result.Cursor.ServiceTier, Usage: usage}
		if rate := payload.RateLimits; rate != nil {
			observation.Allowance = &AllowanceObservation{rate.LimitID, rate.PlanType, rate.Primary.window(), rate.Secondary.window()}
		}
		if observation.Usage != nil || observation.Allowance != nil {
			result.Observations = append(result.Observations, observation)
		}
	}
	return result, nil
}
