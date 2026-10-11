package main

import (
	"fmt"
	"log"
	"net/http"

	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/clientcapability"
	"github.com/josh/codex-switch/internal/menupb"
	"github.com/josh/codex-switch/internal/routing"
	"github.com/josh/codex-switch/internal/store"
	"github.com/josh/codex-switch/internal/usage"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const menuSnapshotEndpoint = "/codex-switch/menu-snapshot"

func menuSnapshotOrRelay(capability, dataDirectory string, router *routing.TaskRouter, usageLedger *usage.UsageLedger, next http.Handler) http.Handler {
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
		snapshot, err := readServingMenuSnapshot(dataDirectory, router, r.URL.Query().Get("model"))
		if err != nil {
			log.Printf("could not build the status snapshot for the menu or status command: %v", err)
			http.Error(w, "snapshot unavailable", http.StatusServiceUnavailable)
			return
		}
		var accountKeys []string
		for _, account := range snapshot.Accounts {
			accountKeys = append(accountKeys, account.Key)
		}
		snapshot.CompletedTasks, snapshot.ProxyActivity, err = usageLedger.ReadRoutedWork(accountKeys)
		if err != nil {
			log.Printf("could not build the status snapshot for the menu or status command: %v", err)
			http.Error(w, "snapshot unavailable", http.StatusServiceUnavailable)
			return
		}
		snapshot.ReportingPeriods, err = usageLedger.ReadReportingPeriods()
		if err != nil {
			log.Printf("could not read account usage for the status snapshot: %v", err)
			http.Error(w, "snapshot unavailable", http.StatusServiceUnavailable)
			return
		}
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
