package accounting

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/josh/codex-switch/internal/codexusage"
	"github.com/josh/codex-switch/internal/pricing"
	"github.com/josh/codex-switch/internal/store"
)

type TaskTotal struct {
	TaskID          string
	System          bool
	Tokens          int64
	Cost            *pricing.NanoUSD
	Accounts        []store.AccountName
	LastCompletedAt time.Time
}

// TotalTasks assigns child work to its originating task before summing a period.
func TotalTasks(completions []Completion, metadata []codexusage.TaskMetadata, start, end time.Time) ([]TaskTotal, error) {
	byTask := make(map[string]codexusage.TaskMetadata)
	for _, task := range metadata {
		byTask[task.TaskID] = task
	}
	turnOwner := make(map[string]string)
	for _, completion := range completions {
		if source := completion.NativeSource; source != nil && source.TurnID != "" {
			turnOwner[source.TurnID] = source.TaskID
		}
	}
	totals := make(map[string]*TaskTotal)
	tasksWithUnattributedUsage := make(map[string]bool)
	for _, completion := range completions {
		if completion.At.Before(start) || !completion.At.Before(end) {
			continue
		}
		owner := completion.TaskID
		unresolvedOrigin := false
		if source := completion.NativeSource; source != nil && source.RootTurnID != "" {
			if origin := turnOwner[source.RootTurnID]; origin != "" {
				owner = origin
			} else {
				unresolvedOrigin = true
			}
		}
		visited := make(map[string]bool)
		for {
			if visited[owner] {
				return nil, fmt.Errorf("cyclic task parent relationship at %s", owner)
			}
			visited[owner] = true
			parent := byTask[owner].ParentTaskID
			if parent == "" {
				parent = byTask[owner].ForkedFromTaskID
			}
			if parent == "" {
				break
			}
			owner = parent
		}
		system := !unresolvedOrigin && strings.HasPrefix(byTask[owner].SourceKind, "subagent:") && byTask[owner].ParentTaskID == ""
		if system {
			owner = "system"
		}
		total := totals[owner]
		if total == nil {
			zero := pricing.NanoUSD(0)
			total = &TaskTotal{TaskID: owner, System: system, Cost: &zero}
			totals[owner] = total
		}
		total.Tokens += completion.Tokens
		if completion.AccountName == "" {
			tasksWithUnattributedUsage[owner] = true
		}
		if completion.Cost == nil {
			total.Cost = nil
		} else if total.Cost != nil {
			*total.Cost += *completion.Cost
		}
		if completion.AccountName != "" && !slices.Contains(total.Accounts, completion.AccountName) {
			total.Accounts = append(total.Accounts, completion.AccountName)
		}
		if completion.At.After(total.LastCompletedAt) {
			total.LastCompletedAt = completion.At
		}
	}
	result := make([]TaskTotal, 0, len(totals))
	for _, total := range totals {
		if tasksWithUnattributedUsage[total.TaskID] {
			total.Accounts = nil
		}
		slices.Sort(total.Accounts)
		result = append(result, *total)
	}
	slices.SortFunc(result, func(a, b TaskTotal) int { return b.LastCompletedAt.Compare(a.LastCompletedAt) })
	return result, nil
}
