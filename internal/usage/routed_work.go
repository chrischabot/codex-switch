package usage

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/josh/codex-switch/internal/menupb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ReadRoutedWork reports work that went through this proxy: the ten most recent
// tasks of the last 30 days, and today's requests, replies, tokens and tasks per
// account. accountKeys lists the pool accounts, which appear even when idle.
func (ledger *UsageLedger) ReadRoutedWork(accountKeys []string) ([]*menupb.CompletedTask, *menupb.ProxyActivity, error) {
	now := time.Now().UTC()
	records, err := ledger.database.ReadCompletedResponsesSince(now.AddDate(0, 0, -30))
	if err != nil {
		return nil, nil, err
	}
	metadata, err := ledger.readTaskMetadata(context.Background())
	if err != nil {
		return nil, nil, err
	}
	completions := make([]reconciledCompletion, 0, len(records))
	for _, record := range records {
		completions = append(completions, reconciledCompletion{TaskID: string(record.CodexTaskID), At: record.CompletedAt, AccountName: record.AccountName, Tokens: record.InputTokens + record.OutputTokens})
	}
	recentTotals, err := totalTasks(completions, metadata, now.AddDate(0, 0, -30), now)
	if err != nil {
		return nil, nil, err
	}
	var recentTasks []*menupb.CompletedTask
	for _, total := range recentTotals {
		if len(recentTasks) == 10 {
			break
		}
		task := completedTask(total)
		task.Tokens = nil
		recentTasks = append(recentTasks, task)
	}
	localNow := now.Local()
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
	activity := &menupb.ProxyActivity{StartedAt: timestamppb.New(start), EndedAt: timestamppb.New(now)}
	accountActivity := make(map[string]*menupb.ProxyAccountActivity)
	for _, key := range accountKeys {
		accountActivity[key] = &menupb.ProxyAccountActivity{AccountKey: key}
	}
	requestsStartedAt, requestCounts, err := ledger.database.ReadModelRequestCounts()
	if err != nil {
		return nil, nil, err
	}
	if !requestsStartedAt.IsZero() {
		activity.RequestsStartedAt = timestamppb.New(requestsStartedAt)
	}
	for _, count := range requestCounts {
		key := string(count.AccountName)
		if accountActivity[key] == nil {
			accountActivity[key] = &menupb.ProxyAccountActivity{AccountKey: key}
		}
		accountActivity[key].RequestCounts = &menupb.ModelRequestCounts{Requests: count.Requests, Successful: count.Successful, Failed: count.Failed, Interrupted: count.Interrupted}
	}
	for _, record := range records {
		if record.CompletedAt.Before(start) || !record.CompletedAt.Before(now) {
			continue
		}
		key := string(record.AccountName)
		if accountActivity[key] == nil {
			accountActivity[key] = &menupb.ProxyAccountActivity{AccountKey: key}
		}
		accountActivity[key].CompletedResponses++
		accountActivity[key].Tokens += record.InputTokens + record.OutputTokens
	}
	for _, account := range accountActivity {
		activity.Accounts = append(activity.Accounts, account)
	}
	slices.SortFunc(activity.Accounts, func(a, b *menupb.ProxyAccountActivity) int { return cmp.Compare(a.AccountKey, b.AccountKey) })
	todayTotals, err := totalTasks(completions, metadata, start, now)
	if err != nil {
		return nil, nil, err
	}
	for _, total := range todayTotals {
		if len(activity.Tasks) == 10 {
			break
		}
		activity.Tasks = append(activity.Tasks, completedTask(total))
	}
	return recentTasks, activity, nil
}
