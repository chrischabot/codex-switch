package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	accountapi "github.com/joshp123/codex-switch/internal/accounts"
	"github.com/joshp123/codex-switch/internal/clientcapability"
	"github.com/joshp123/codex-switch/internal/codexstate"
	"github.com/joshp123/codex-switch/internal/menupb"
	"github.com/joshp123/codex-switch/internal/routing"
	"github.com/joshp123/codex-switch/internal/store"
	"github.com/joshp123/codex-switch/internal/usage"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const menuSnapshotEndpoint = "/codex-switch/menu-snapshot"

// menuSnapshotURLFilename is the file in the data directory that holds the
// serving proxy's private menu snapshot URL. The menu app and the status
// command read it; each generation writes it once it listens.
const menuSnapshotURLFilename = "menu-snapshot-url"

const menuSnapshotInterval = time.Minute

// menuSnapshots composes the menu snapshot once a minute and serves the
// latest one, so a reader never waits for accounts, usage or task names.
type menuSnapshots struct {
	dataDirectory string
	// codexBaseURL is the openai_base_url that sends Codex work to this proxy.
	codexBaseURL string
	router       *routing.TaskRouter
	usageLedger  *usage.UsageLedger
	codex        *codexstate.LocalReader

	mutex   sync.Mutex
	encoded []byte
}

func (snapshots *menuSnapshots) Run(ctx context.Context) {
	ticker := time.NewTicker(menuSnapshotInterval)
	defer ticker.Stop()
	for {
		if err := snapshots.compose(ctx); err != nil && ctx.Err() == nil {
			log.Printf("could not compose the menu snapshot: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (snapshots *menuSnapshots) compose(ctx context.Context) error {
	installed, err := accountapi.ReadPool(snapshots.dataDirectory)
	if err != nil {
		return err
	}
	hasChatGPTAccounts := slices.ContainsFunc(installed, func(account accountapi.PoolAccount) bool {
		return account.Provider != accountapi.ProviderClaude
	})
	var config codexstate.EffectiveModelConfig
	if hasChatGPTAccounts {
		configContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		config, err = snapshots.codex.ModelConfig(configContext)
		cancel()
		if err != nil {
			return err
		}
	}
	snapshot, err := readServingMenuSnapshot(snapshots.dataDirectory, snapshots.router, config.Model)
	if err != nil {
		return err
	}
	snapshot.Connection.NewTasksConfigured = config.Provider == "openai" && config.OpenAIBaseURL != nil && strings.TrimRight(*config.OpenAIBaseURL, "/") == snapshots.codexBaseURL
	var accountKeys []string
	for _, account := range snapshot.Accounts {
		accountKeys = append(accountKeys, account.Key)
	}
	if snapshot.ProxyActivity, err = snapshots.usageLedger.ReadRoutedWork(accountKeys); err != nil {
		return err
	}
	if snapshot.ReportingPeriods, snapshot.AccountDays, err = snapshots.usageLedger.TotalUsage(ctx); err != nil {
		return err
	}
	if hasChatGPTAccounts {
		if err := nameCodexTasks(ctx, snapshots.codex, snapshot); err != nil {
			return err
		}
	}
	encoded, err := proto.Marshal(snapshot)
	if err != nil {
		return err
	}
	snapshots.mutex.Lock()
	snapshots.encoded = encoded
	snapshots.mutex.Unlock()
	return nil
}

// nameCodexTasks gives each task the name Codex knows it by. A task Codex
// has no record of keeps no name.
func nameCodexTasks(ctx context.Context, codex *codexstate.LocalReader, snapshot *menupb.MenuSnapshot) error {
	tasks := slices.Clone(snapshot.GetProxyActivity().GetTasks())
	for _, period := range snapshot.ReportingPeriods {
		tasks = append(tasks, period.GetTaskUsage().GetTasks()...)
	}
	var ids []string
	for _, task := range tasks {
		if task.Title == nil && task.TaskId != "" && !slices.Contains(ids, task.TaskId) {
			ids = append(ids, task.TaskId)
		}
	}
	titles, err := codex.TaskTitles(ctx, ids)
	if err != nil {
		return fmt.Errorf("read Codex task names: %w", err)
	}
	for _, task := range tasks {
		if title, ok := titles[task.TaskId]; ok && task.Title == nil {
			task.Title = &title
		}
	}
	return nil
}

func (snapshots *menuSnapshots) serveOrRelay(capability string, next http.Handler) http.Handler {
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
		snapshots.mutex.Lock()
		encoded := snapshots.encoded
		snapshots.mutex.Unlock()
		if encoded == nil {
			http.Error(w, "the first menu snapshot is not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(encoded)
	})
}

// writeMenuSnapshotURL publishes the URL atomically and readable only by
// this user, because it carries the client capability.
func writeMenuSnapshotURL(dataDirectory, snapshotURL string) error {
	temporary, err := os.CreateTemp(dataDirectory, ".menu-snapshot-url-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.WriteString(snapshotURL + "\n"); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), filepath.Join(dataDirectory, menuSnapshotURLFilename))
}

// readServingMenuSnapshot reports every installed account: its allowance,
// whether it can take new work for modelSlug and why not.
func readServingMenuSnapshot(dataDirectory string, router *routing.TaskRouter, modelSlug string) (*menupb.MenuSnapshot, error) {
	facts, unavailableReasons, err := router.ReadAccountAvailability(modelSlug)
	if err != nil {
		return nil, err
	}
	installed, err := accountapi.ReadPool(dataDirectory)
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
		account := &menupb.Account{Key: string(fact.AccountName), AccountNumber: uint32(identity.ProviderNumber), Email: identity.Email, Plan: sample.Plan, State: menuAccountState(fact.AccountState), Usable: unavailableReasons[fact.AccountName] == "", UnavailableReason: unavailableReasons[fact.AccountName], OffersConfiguredModel: modelSlug == "" || slices.Contains(fact.SupportedModelSlugs, modelSlug)}
		if account.Usable {
			account.ReserveStatus = accountapi.ReserveStatus(fact, snapshot.GeneratedAt.AsTime())
		}
		account.Provider = menupb.Provider_PROVIDER_CHATGPT
		if identity.Provider == accountapi.ProviderClaude {
			account.Provider = menupb.Provider_PROVIDER_CLAUDE
			account.OffersConfiguredModel = true
		}
		if !sample.ObservedAt.IsZero() {
			observation := &menupb.AllowanceObservation{ObservedAt: timestamppb.New(sample.ObservedAt), Allowed: sample.Allowed, LimitReached: sample.LimitReached, AvailableResetCredits: sample.AvailableResetCredits}
			for _, window := range sample.Windows {
				observation.Windows = append(observation.Windows, &menupb.AllowanceWindow{PeriodSeconds: window.DurationSeconds, UsedPercent: window.UsedPercent, ResetAt: timestamppb.New(window.ResetsAt), ModelId: window.ModelID, ModelName: window.ModelName})
			}
			account.Allowance = observation
		}
		snapshot.Accounts = append(snapshot.Accounts, account)
	}
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

// servedListenAddress is where clients reach this installation: the address
// its proxy last served on, or the default before it first serves.
func servedListenAddress(dataDirectory string) string {
	published, err := os.ReadFile(filepath.Join(dataDirectory, menuSnapshotURLFilename))
	if err != nil {
		return listenAddress
	}
	snapshotURL, err := url.Parse(strings.TrimSpace(string(published)))
	if err != nil || validateLocalListenAddress(snapshotURL.Host) != nil {
		return listenAddress
	}
	return snapshotURL.Host
}
