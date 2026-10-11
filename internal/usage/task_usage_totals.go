package usage

import (
	"context"
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

type taskUsagePeriod struct{ start, end int64 }

type taskUsageTotals struct {
	tasks           []taskTotal
	tokensByAccount map[string]int64
	costsByAccount  map[string]int64
	fullyAttributed bool
	fullyPriced     bool
}

// refreshTaskUsage scans local Codex and Pi session history, joins it with the
// responses the proxy recorded, and totals each reporting period by account and task.
func (ledger *UsageLedger) refreshTaskUsage(ctx context.Context) {
	if err := ledger.totalTaskUsage(ctx); err != nil && ctx.Err() == nil {
		log.Printf("could not total usage per task: %v", err)
	}
}

func (ledger *UsageLedger) totalTaskUsage(ctx context.Context) error {
	reader, err := codexstate.DefaultReader()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -29)
	periods, err := ledger.readProviderReportingPeriods()
	if err != nil {
		return err
	}
	for _, period := range periods {
		if period.StartedAt.AsTime().Before(start) {
			start = period.StartedAt.AsTime()
		}
	}
	if _, err := ledger.importCodexSessions(ctx, reader.CodexHome, start); err != nil {
		return err
	}
	piSessions := os.Getenv("PI_CODING_AGENT_SESSION_DIR")
	if piSessions == "" {
		piDirectory := os.Getenv("PI_CODING_AGENT_DIR")
		if piDirectory == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			piDirectory = filepath.Join(home, ".pi", "agent")
		}
		piSessions = filepath.Join(piDirectory, "sessions")
	}
	if _, err := ledger.importPiSessions(ctx, piSessions, start); err != nil {
		return err
	}
	periods, err = ledger.readProviderReportingPeriods()
	if err != nil || len(periods) == 0 {
		return err
	}
	native, err := ledger.readSessionUsageObservations(ctx, start, time.Now().UTC())
	if err != nil {
		return err
	}
	metadata, err := ledger.readTaskMetadata(ctx)
	if err != nil {
		return err
	}
	responses, err := ledger.database.ReadCompletedResponsesSince(start)
	if err != nil {
		return err
	}
	completions, err := reconcileCompletions(native, responses)
	if err != nil {
		return err
	}
	identities, err := ledger.database.ReadAccountIdentities()
	if err != nil {
		return err
	}
	installed, err := accounts.ReadPool(ledger.dataDirectory)
	if err != nil {
		return err
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
		return err
	}
	for index := range completions {
		if name := namesByProvider[completions[index].ProviderAccountID]; name != "" {
			completions[index].AccountName = name
		}
	}
	totals := make(map[taskUsagePeriod]taskUsageTotals)
	for _, period := range periods {
		begin, end := period.StartedAt.AsTime(), period.EndedAt.AsTime()
		if begin.Before(start) {
			continue
		}
		tasks, err := totalTasks(completions, metadata, begin, end)
		if err != nil {
			return err
		}
		report := taskUsageTotals{tasks: tasks, tokensByAccount: make(map[string]int64), costsByAccount: make(map[string]int64), fullyAttributed: true, fullyPriced: true}
		for _, completion := range completions {
			if completion.At.Before(begin) || !completion.At.Before(end) {
				continue
			}
			if completion.AccountName == "" {
				report.fullyAttributed = false
			}
			account := string(completion.AccountName)
			report.tokensByAccount[account] += completion.Tokens
			if completion.Cost == nil {
				report.fullyPriced = false
			} else {
				report.costsByAccount[account] += int64(*completion.Cost)
			}
		}
		totals[taskUsagePeriod{begin.Unix(), end.Unix()}] = report
	}
	ledger.taskUsageMutex.Lock()
	ledger.taskUsageByPeriod = totals
	ledger.taskUsageMutex.Unlock()
	return nil
}

// addTaskUsage adds account costs and per-task totals to periods whose
// local totals reconcile with OpenAI's per-account totals.
func (ledger *UsageLedger) addTaskUsage(periods []*menupb.ReportingPeriod) {
	ledger.taskUsageMutex.Lock()
	defer ledger.taskUsageMutex.Unlock()
	for _, period := range periods {
		key := taskUsagePeriod{period.StartedAt.AsTime().Unix(), period.EndedAt.AsTime().Unix()}
		totals, found := ledger.taskUsageByPeriod[key]
		if !found || !totals.matchesAccounts(period.Accounts) {
			continue
		}
		period.TaskUsage = &menupb.TaskUsageReport{}
		if totals.fullyPriced {
			for _, account := range period.Accounts {
				cost := totals.costsByAccount[account.AccountKey]
				account.CostNanoUsd = &cost
			}
		}
		for _, total := range totals.tasks {
			tokens := total.Tokens
			task := &menupb.CompletedTask{TaskId: total.TaskID, Tokens: &tokens, LastCompletedAt: timestamppb.New(total.LastCompletedAt)}
			if total.System {
				title := "System"
				task.Title = &title
			}
			if total.Cost != nil {
				cost := int64(*total.Cost)
				task.CostNanoUsd = &cost
			}
			for _, account := range total.Accounts {
				task.AccountKeys = append(task.AccountKeys, string(account))
			}
			period.TaskUsage.Tasks = append(period.TaskUsage.Tasks, task)
		}
	}
}

// Task amounts and account totals share one report only when they reconcile.
func (totals taskUsageTotals) matchesAccounts(providerTotals []*menupb.AccountTotal) bool {
	if !totals.fullyAttributed {
		return false
	}
	remaining := make(map[string]int64, len(totals.tokensByAccount))
	for account, tokens := range totals.tokensByAccount {
		remaining[account] = tokens
	}
	for _, account := range providerTotals {
		if remaining[account.AccountKey] != account.Tokens {
			return false
		}
		delete(remaining, account.AccountKey)
	}
	return len(remaining) == 0
}
