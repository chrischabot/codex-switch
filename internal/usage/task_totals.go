package usage

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/joshp123/codex-switch/internal/store"
)

type taskTotal struct {
	TaskID          string
	Title           string
	Tokens          int64
	Cost            *nanoUSD
	Accounts        []store.AccountName
	LastCompletedAt time.Time
}

// totalTasks assigns child work to its originating task before summing a period.
func totalTasks(completions []reconciledCompletion, metadata []taskMetadata, start, end time.Time) ([]taskTotal, error) {
	byTask := make(map[string]taskMetadata)
	for _, task := range metadata {
		byTask[task.TaskID] = task
	}
	turnOwner := make(map[string]string)
	for _, completion := range completions {
		if source := completion.NativeSource; source != nil && source.TurnID != "" {
			turnOwner[source.TurnID] = source.TaskID
		}
	}
	totals := make(map[string]*taskTotal)
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
		system := owner == "" || (!unresolvedOrigin && strings.HasPrefix(byTask[owner].SourceKind, "subagent:") && byTask[owner].ParentTaskID == "")
		if system {
			owner = "system"
		}
		total := totals[owner]
		if total == nil {
			zero := nanoUSD(0)
			total = &taskTotal{TaskID: owner, Title: localTaskTitle(owner, system, byTask[owner]), Cost: &zero}
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
	result := make([]taskTotal, 0, len(totals))
	for _, total := range totals {
		if tasksWithUnattributedUsage[total.TaskID] {
			total.Accounts = nil
		}
		slices.Sort(total.Accounts)
		result = append(result, *total)
	}
	slices.SortFunc(result, func(a, b taskTotal) int { return b.LastCompletedAt.Compare(a.LastCompletedAt) })
	return result, nil
}
