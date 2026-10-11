package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/josh/codex-switch/internal/accounting"
	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/codexstate"
	"github.com/josh/codex-switch/internal/menupb"
	"github.com/josh/codex-switch/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type taskUsagePeriod struct{ start, end int64 }

type taskUsageTotals struct {
	tasks           []accounting.TaskTotal
	tokensByAccount map[string]int64
	costsByAccount  map[string]int64
	fullyAttributed bool
	fullyPriced     bool
}

type taskUsageSource struct {
	database      *store.StoreDatabase
	profiles      *poolAccountUsageSource
	dataDirectory string
	mutex         sync.Mutex
	totals        map[taskUsagePeriod]taskUsageTotals
}

func newTaskUsageSource(database *store.StoreDatabase, profiles *poolAccountUsageSource, directory string) *taskUsageSource {
	return &taskUsageSource{database: database, profiles: profiles, dataDirectory: directory, totals: make(map[taskUsagePeriod]taskUsageTotals)}
}

func (source *taskUsageSource) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := source.refresh(ctx); err != nil && ctx.Err() == nil {
			log.Printf("could not total usage per task: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (source *taskUsageSource) refresh(ctx context.Context) error {
	reader, err := codexstate.DefaultReader()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -29)
	periods, err := source.profiles.ReadReportingPeriods()
	if err != nil {
		return err
	}
	for _, period := range periods {
		if period.StartedAt.AsTime().Before(start) {
			start = period.StartedAt.AsTime()
		}
	}
	if _, err := source.database.ImportCodexUsage(ctx, reader.CodexHome, start); err != nil {
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
	if _, err := source.database.ImportPiUsage(ctx, piSessions, start); err != nil {
		return err
	}
	periods, err = source.profiles.ReadReportingPeriods()
	if err != nil || len(periods) == 0 {
		return err
	}
	native, err := source.database.ReadNativeUsageObservations(ctx, start, time.Now().UTC())
	if err != nil {
		return err
	}
	metadata, err := source.database.ReadNativeTaskMetadata(ctx)
	if err != nil {
		return err
	}
	responses, err := source.database.ReadCompletedResponsesSince(start)
	if err != nil {
		return err
	}
	completions, err := accounting.ReconcileCompletions(native, responses)
	if err != nil {
		return err
	}
	identities, err := source.database.ReadAccountIdentities()
	if err != nil {
		return err
	}
	installed, err := accountapi.ReadPool(source.dataDirectory)
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
	if err := accounting.AttributeAccounts(completions, native, identities, source.database); err != nil {
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
		tasks, err := accounting.TotalTasks(completions, metadata, begin, end)
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
	source.mutex.Lock()
	source.totals = totals
	source.mutex.Unlock()
	return nil
}

func (source *taskUsageSource) AddTasks(periods []*menupb.ReportingPeriod) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	for _, period := range periods {
		key := taskUsagePeriod{period.StartedAt.AsTime().Unix(), period.EndedAt.AsTime().Unix()}
		totals, found := source.totals[key]
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
func (totals taskUsageTotals) matchesAccounts(accounts []*menupb.AccountTotal) bool {
	if !totals.fullyAttributed {
		return false
	}
	remaining := make(map[string]int64, len(totals.tokensByAccount))
	for account, tokens := range totals.tokensByAccount {
		remaining[account] = tokens
	}
	for _, account := range accounts {
		if remaining[account.AccountKey] != account.Tokens {
			return false
		}
		delete(remaining, account.AccountKey)
	}
	return len(remaining) == 0
}
