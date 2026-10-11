package usage

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/codexstate"
	"github.com/josh/codex-switch/internal/menupb"
	"github.com/josh/codex-switch/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// refreshReportingPeriods scans local Codex and Pi session history, joins it
// with the responses the proxy recorded and OpenAI's per-account report, and
// totals Today, 7 Days and 30 Days by account and by task.
func (ledger *UsageLedger) refreshReportingPeriods(ctx context.Context) {
	periods, err := ledger.totalReportingPeriods(ctx, time.Now().UTC())
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("could not total usage per account and task: %v", err)
		}
		return
	}
	ledger.periodsMutex.Lock()
	ledger.periods = periods
	ledger.periodsMutex.Unlock()
}

func (ledger *UsageLedger) totalReportingPeriods(ctx context.Context, now time.Time) ([]*menupb.ReportingPeriod, error) {
	today := dateOnly(now)
	start := today.AddDate(0, 0, 1-reportingPeriodDays)
	completions, metadata, err := ledger.readLocalCompletions(ctx, start, now)
	if err != nil {
		return nil, err
	}
	pool, err := accounts.ReadPool(ledger.dataDirectory)
	if err != nil {
		return nil, err
	}
	dailyUsage, err := ledger.readAccountDailyUsage(pool, completions)
	if err != nil {
		return nil, err
	}
	periods := make([]*menupb.ReportingPeriod, 0, 3)
	for _, definition := range []struct {
		kind menupb.ReportingPeriodKind
		days int
	}{
		{menupb.ReportingPeriodKind_REPORTING_PERIOD_TODAY, 1},
		{menupb.ReportingPeriodKind_REPORTING_PERIOD_SEVEN_DAYS, 7},
		{menupb.ReportingPeriodKind_REPORTING_PERIOD_THIRTY_DAYS, reportingPeriodDays},
	} {
		begin, end := today.AddDate(0, 0, 1-definition.days), today.AddDate(0, 0, 1)
		period := &menupb.ReportingPeriod{Kind: definition.kind, StartedAt: timestamppb.New(begin), EndedAt: timestamppb.New(end), TaskUsage: &menupb.TaskUsageReport{}}
		for _, account := range pool {
			var total usageAmount
			for _, date := range periodDates(today, definition.days) {
				total.tokens += dailyUsage[accountDay{account.Name, date}].tokens
				total.cost += dailyUsage[accountDay{account.Name, date}].cost
			}
			cost := int64(total.cost)
			period.Accounts = append(period.Accounts, &menupb.AccountTotal{AccountKey: account.Name, Tokens: total.tokens, CostNanoUsd: &cost})
		}
		tasks, err := totalTasks(completions, metadata, begin, end)
		if err != nil {
			return nil, err
		}
		for _, total := range tasks {
			period.TaskUsage.Tasks = append(period.TaskUsage.Tasks, completedTask(total))
		}
		periods = append(periods, period)
	}
	return periods, nil
}

// readAccountDailyUsage merges OpenAI's per-account report with this Mac's
// attributed responses for each pool account and UTC date.
func (ledger *UsageLedger) readAccountDailyUsage(pool []accounts.PoolAccount, completions []reconciledCompletion) (map[accountDay]usageAmount, error) {
	histories, err := ledger.readOpenAIDailyTokenHistories()
	if err != nil {
		return nil, err
	}
	shares, err := ledger.readOpenAIAllowanceShares()
	if err != nil {
		return nil, err
	}
	local := make(map[string]map[string]usageAmount)
	for _, completion := range completions {
		account := string(completion.AccountName)
		if account == "" {
			continue
		}
		if local[account] == nil {
			local[account] = make(map[string]usageAmount)
		}
		date := completion.At.UTC().Format(time.DateOnly)
		amount := local[account][date]
		amount.tokens += completion.Tokens
		if completion.Cost != nil {
			amount.cost += *completion.Cost
		} else {
			ledger.logOnChange("price/"+completion.Model, fmt.Sprintf("model %s has no API-equivalent price; add it to the rate card", completion.Model))
		}
		local[account][date] = amount
	}
	localStandardCost, err := ledger.readProxyStandardCostByModelDay(shares)
	if err != nil {
		return nil, err
	}
	daily := make(map[accountDay]usageAmount)
	for _, account := range pool {
		openAITokens := make(map[string]int64)
		for _, bucket := range histories[account.AccountID].Buckets {
			openAITokens[bucket.StartDate] = bucket.Tokens
		}
		accountShares := shares[account.AccountID]
		value, agreeing := allowanceValuePerPercent(accountShares, localStandardCost[account.Name])
		if value > 0 {
			ledger.logOnChange("value/"+account.Name, fmt.Sprintf("%s: 1%% of the allowance is worth $%.4f at API prices, agreed by %d model-days served only through this Mac", account.Name, value/1e9, agreeing))
		} else if len(accountShares) > 0 {
			ledger.logOnChange("value/"+account.Name, fmt.Sprintf("%s: cannot price the use OpenAI reports from elsewhere: fewer than two model-days were served only through this Mac", account.Name))
		}
		for date, amount := range accountDailyUsage(openAITokens, openAIDailyCost(accountShares, value), local[account.Name]) {
			daily[accountDay{account.Name, date}] = amount
		}
	}
	return daily, nil
}

// readProxyStandardCostByModelDay prices every Standard response the proxy
// recorded since the oldest stored allowance share, by account and model-day.
// Calibration reads all of this retained history, so the allowance value
// survives after the days that established it leave the 30-day window.
func (ledger *UsageLedger) readProxyStandardCostByModelDay(shares map[string][]accounts.OpenAIAllowanceShare) (map[string]map[modelDay]nanoUSD, error) {
	oldest := ""
	for _, accountShares := range shares {
		for _, share := range accountShares {
			if oldest == "" || share.Date < oldest {
				oldest = share.Date
			}
		}
	}
	costs := make(map[string]map[modelDay]nanoUSD)
	if oldest == "" {
		return costs, nil
	}
	since, err := time.Parse(time.DateOnly, oldest)
	if err != nil {
		return nil, err
	}
	responses, err := ledger.database.ReadCompletedResponsesSince(since)
	if err != nil {
		return nil, err
	}
	for _, response := range responses {
		if response.HarnessMode == string(harnessFast) {
			continue
		}
		cost := apiEquivalentPrice(response.ModelSlug, harnessStandard, tokenUsage{InputTokens: response.InputTokens, CachedInputTokens: response.CachedInputTokens, OutputTokens: response.OutputTokens})
		if cost == nil {
			continue
		}
		account := string(response.AccountName)
		if costs[account] == nil {
			costs[account] = make(map[modelDay]nanoUSD)
		}
		costs[account][modelDay{response.CompletedAt.UTC().Format(time.DateOnly), response.ModelSlug, false}] += *cost
	}
	return costs, nil
}

// readLocalCompletions imports new local session history and returns every
// response this Mac saw from start, attributed to its account where known.
func (ledger *UsageLedger) readLocalCompletions(ctx context.Context, start, now time.Time) ([]reconciledCompletion, []taskMetadata, error) {
	reader, err := codexstate.DefaultReader()
	if err != nil {
		return nil, nil, err
	}
	if _, err := ledger.importCodexSessions(ctx, reader.CodexHome, start); err != nil {
		return nil, nil, err
	}
	piSessions, err := piSessionDirectory()
	if err != nil {
		return nil, nil, err
	}
	if _, err := ledger.importPiSessions(ctx, piSessions, start); err != nil {
		return nil, nil, err
	}
	native, err := ledger.readSessionUsageObservations(ctx, start, now)
	if err != nil {
		return nil, nil, err
	}
	metadata, err := ledger.readTaskMetadata(ctx)
	if err != nil {
		return nil, nil, err
	}
	responses, err := ledger.database.ReadCompletedResponsesSince(start)
	if err != nil {
		return nil, nil, err
	}
	completions, err := reconcileCompletions(native, responses)
	if err != nil {
		return nil, nil, err
	}
	identities, err := ledger.database.ReadAccountIdentities()
	if err != nil {
		return nil, nil, err
	}
	installed, err := accounts.ReadPool(ledger.dataDirectory)
	if err != nil {
		return nil, nil, err
	}
	namesByProvider := make(map[store.ProviderAccountID]store.AccountName)
	for _, identity := range identities {
		namesByProvider[store.ProviderAccountID(identity.ProviderAccountID)] = identity.AccountName
	}
	for _, account := range installed {
		identity := store.AccountIdentity{AccountName: store.AccountName(account.Name), ProviderAccountID: account.AccountID, Email: account.Email}
		identities = append(identities, identity)
		namesByProvider[store.ProviderAccountID(account.AccountID)] = identity.AccountName
	}
	if err := attributeAccounts(completions, native, identities, ledger.database); err != nil {
		return nil, nil, err
	}
	for index := range completions {
		if name := namesByProvider[completions[index].ProviderAccountID]; name != "" {
			completions[index].AccountName = name
		}
	}
	return completions, metadata, nil
}

func piSessionDirectory() (string, error) {
	if directory := os.Getenv("PI_CODING_AGENT_SESSION_DIR"); directory != "" {
		return directory, nil
	}
	piDirectory := os.Getenv("PI_CODING_AGENT_DIR")
	if piDirectory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		piDirectory = filepath.Join(home, ".pi", "agent")
	}
	return filepath.Join(piDirectory, "sessions"), nil
}

func completedTask(total taskTotal) *menupb.CompletedTask {
	tokens := total.Tokens
	task := &menupb.CompletedTask{TaskId: total.TaskID, Tokens: &tokens, LastCompletedAt: timestamppb.New(total.LastCompletedAt)}
	if total.Title != "" {
		title := total.Title
		task.Title = &title
	}
	if total.Cost != nil {
		cost := int64(*total.Cost)
		task.CostNanoUsd = &cost
	}
	for _, account := range total.Accounts {
		task.AccountKeys = append(task.AccountKeys, string(account))
	}
	return task
}
