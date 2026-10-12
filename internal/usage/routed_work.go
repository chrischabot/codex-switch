package usage

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/joshp123/codex-switch/internal/menupb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ReadRoutedWork reports today's work through this proxy: requests, replies
// and tokens per account, and the ten most recent tasks. accountKeys lists
// the pool accounts, which appear even when idle.
func (ledger *UsageLedger) ReadRoutedWork(accountKeys []string) (*menupb.ProxyActivity, error) {
	now := time.Now().UTC()
	localNow := now.Local()
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
	records, err := ledger.database.ReadCompletedResponsesSince(start)
	if err != nil {
		return nil, err
	}
	claudeMetadata, err := claudeTaskMetadata(records)
	if err != nil {
		return nil, err
	}
	metadata, err := ledger.readTaskMetadata(context.Background())
	if err != nil {
		return nil, err
	}
	metadata = append(metadata, claudeMetadata...)
	activity := &menupb.ProxyActivity{StartedAt: timestamppb.New(start), EndedAt: timestamppb.New(now)}
	accountActivity := make(map[string]*menupb.ProxyAccountActivity)
	for _, key := range accountKeys {
		accountActivity[key] = &menupb.ProxyAccountActivity{AccountKey: key}
	}
	requestsStartedAt, requestCounts, err := ledger.database.ReadModelRequestCounts()
	if err != nil {
		return nil, err
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
	completions := make([]reconciledCompletion, 0, len(records))
	for _, record := range records {
		if !record.CompletedAt.Before(now) {
			continue
		}
		key := string(record.AccountName)
		if accountActivity[key] == nil {
			accountActivity[key] = &menupb.ProxyAccountActivity{AccountKey: key}
		}
		accountActivity[key].CompletedResponses++
		accountActivity[key].Tokens += record.InputTokens + record.OutputTokens
		cost := apiEquivalentPrice(record.ModelSlug, harnessMode(record.HarnessMode), tokenUsage{InputTokens: record.InputTokens, CachedInputTokens: record.CachedInputTokens, OutputTokens: record.OutputTokens})
		if record.CostNanoUSD != nil {
			price := nanoUSD(*record.CostNanoUSD)
			cost = &price
		}
		completions = append(completions, reconciledCompletion{TaskID: string(record.TaskID), At: record.CompletedAt, AccountName: record.AccountName, Tokens: record.InputTokens + record.OutputTokens, Cost: cost})
	}
	for _, account := range accountActivity {
		activity.Accounts = append(activity.Accounts, account)
	}
	slices.SortFunc(activity.Accounts, func(a, b *menupb.ProxyAccountActivity) int { return cmp.Compare(a.AccountKey, b.AccountKey) })
	todayTotals, err := totalTasks(completions, metadata, start, now)
	if err != nil {
		return nil, err
	}
	for _, total := range todayTotals {
		if len(activity.Tasks) == 10 {
			break
		}
		activity.Tasks = append(activity.Tasks, completedTask(total))
	}
	return activity, nil
}
