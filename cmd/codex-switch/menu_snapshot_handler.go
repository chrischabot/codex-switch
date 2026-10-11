package main

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/josh/codex-switch/internal/accounting"
	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/clientcapability"
	"github.com/josh/codex-switch/internal/menupb"
	"github.com/josh/codex-switch/internal/routing"
	"github.com/josh/codex-switch/internal/store"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const menuSnapshotEndpoint = "/codex-switch/menu-snapshot"

func menuSnapshotOrRelay(capability string, source *poolAccountRoutingFactsSource, router *routing.TaskRouter, database *store.StoreDatabase, usage *poolAccountUsageSource, tasks *taskUsageSource, next http.Handler) http.Handler {
	path := clientcapability.PathPrefix(capability) + menuSnapshotEndpoint
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		snapshot, err := readServingMenuSnapshot(source, router, database, r.URL.Query().Get("model"))
		if err != nil {
			log.Printf("could not build the status snapshot for the menu or status command: %v", err)
			http.Error(w, "snapshot unavailable", http.StatusServiceUnavailable)
			return
		}
		snapshot.ReportingPeriods, err = usage.ReadReportingPeriods()
		if err != nil {
			log.Printf("could not read account usage for the status snapshot: %v", err)
			http.Error(w, "snapshot unavailable", http.StatusServiceUnavailable)
			return
		}
		tasks.AddTasks(snapshot.ReportingPeriods)
		body, err := proto.Marshal(snapshot)
		if err != nil {
			http.Error(w, "snapshot unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	})
}

func readServingMenuSnapshot(source *poolAccountRoutingFactsSource, router *routing.TaskRouter, database *store.StoreDatabase, modelSlug string) (*menupb.MenuSnapshot, error) {
	facts, unavailableReasons, err := router.ReadAccountAvailability(modelSlug)
	if err != nil {
		return nil, err
	}
	installed, err := accountapi.ReadPool(source.dataDirectory)
	if err != nil {
		return nil, err
	}
	if len(facts) != len(installed) {
		return nil, fmt.Errorf("account observations are not ready")
	}
	identities := map[store.AccountName]accountapi.PoolAccount{}
	for _, account := range installed {
		identities[store.AccountName(account.Name)] = account
	}
	snapshot := &menupb.MenuSnapshot{GeneratedAt: timestamppb.Now(), Connection: &menupb.Connection{}}
	for _, fact := range facts {
		identity := identities[fact.AccountName]
		sample := fact.AllowanceSample
		account := &menupb.Account{Key: string(fact.AccountName), AccountNumber: uint32(identity.Index), Email: identity.Email, Plan: sample.Plan, State: menuAccountState(fact.AccountState), Usable: unavailableReasons[fact.AccountName] == "", UnavailableReason: unavailableReasons[fact.AccountName]}
		if !sample.ObservedAt.IsZero() {
			observation := &menupb.AllowanceObservation{ObservedAt: timestamppb.New(sample.ObservedAt), Allowed: sample.Allowed, LimitReached: sample.LimitReached, AvailableResetCredits: sample.AvailableResetCredits}
			for _, window := range sample.Windows {
				observation.Windows = append(observation.Windows, &menupb.AllowanceWindow{PeriodSeconds: window.DurationSeconds, UsedPercent: window.UsedPercent, ResetAt: timestamppb.New(window.ResetsAt)})
			}
			account.Allowance = observation
		}
		snapshot.Accounts = append(snapshot.Accounts, account)
	}
	now := time.Now().UTC()
	records, err := database.ReadCompletedResponsesSince(now.AddDate(0, 0, -30))
	if err != nil {
		return nil, err
	}
	metadata, err := database.ReadNativeTaskMetadata(context.Background())
	if err != nil {
		return nil, err
	}
	completions := make([]accounting.Completion, 0, len(records))
	for _, record := range records {
		completions = append(completions, accounting.Completion{TaskID: string(record.CodexTaskID), At: record.CompletedAt, AccountName: record.AccountName, Tokens: record.InputTokens + record.OutputTokens})
	}
	recentTasks, err := accounting.TotalTasks(completions, metadata, now.AddDate(0, 0, -30), now)
	if err != nil {
		return nil, err
	}
	for _, total := range recentTasks {
		if len(snapshot.CompletedTasks) == 10 {
			break
		}
		task := &menupb.CompletedTask{TaskId: total.TaskID, LastCompletedAt: timestamppb.New(total.LastCompletedAt)}
		if total.System {
			title := "System"
			task.Title = &title
		}
		for _, account := range total.Accounts {
			task.AccountKeys = append(task.AccountKeys, string(account))
		}
		snapshot.CompletedTasks = append(snapshot.CompletedTasks, task)
	}
	localNow := now.Local()
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
	activity := &menupb.ProxyActivity{StartedAt: timestamppb.New(start), EndedAt: timestamppb.New(now)}
	accountActivity := make(map[string]*menupb.ProxyAccountActivity)
	for _, account := range snapshot.Accounts {
		accountActivity[account.Key] = &menupb.ProxyAccountActivity{AccountKey: account.Key}
	}
	requestsStartedAt, requestCounts, err := database.ReadModelRequestCounts()
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
	routedTasks, err := accounting.TotalTasks(completions, metadata, start, now)
	if err != nil {
		return nil, err
	}
	for _, total := range routedTasks {
		if len(activity.Tasks) == 10 {
			break
		}
		task := &menupb.CompletedTask{TaskId: total.TaskID, LastCompletedAt: timestamppb.New(total.LastCompletedAt), Tokens: &total.Tokens}
		if total.System {
			title := "System"
			task.Title = &title
		}
		for _, account := range total.Accounts {
			task.AccountKeys = append(task.AccountKeys, string(account))
		}
		activity.Tasks = append(activity.Tasks, task)
	}
	snapshot.ProxyActivity = activity
	return snapshot, nil
}

func menuAccountState(state accountapi.AccountState) menupb.AccountState {
	switch state {
	case accountapi.AccountReady:
		return menupb.AccountState_ACCOUNT_STATE_READY
	case accountapi.AccountLimited:
		return menupb.AccountState_ACCOUNT_STATE_LIMITED
	case accountapi.AccountSignInRequired:
		return menupb.AccountState_ACCOUNT_STATE_SIGN_IN_REQUIRED
	case accountapi.AccountCredentialsUnavailable:
		return menupb.AccountState_ACCOUNT_STATE_CREDENTIALS_UNAVAILABLE
	case accountapi.AccountUsageCheckFailed:
		return menupb.AccountState_ACCOUNT_STATE_USAGE_CHECK_FAILED
	default:
		return menupb.AccountState_ACCOUNT_STATE_UNSPECIFIED
	}
}
