// Package usage answers how many tokens each account and each task used, and
// what that work would cost at OpenAI's API prices.
//
// It reads OpenAI's daily token history for every pool account, scans local
// Codex and Pi session history, and joins both with the responses the relay
// recorded. It keeps its own tables in the store's database.
package usage

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/josh/codex-switch/internal/menupb"
	"github.com/josh/codex-switch/internal/store"
)

const usageRefreshInterval = time.Minute

type UsageLedger struct {
	database      *store.StoreDatabase
	sqlDatabase   *sql.DB
	dataDirectory string
	httpClient    *http.Client

	problemMutex      sync.Mutex
	lastLoggedProblem map[string]string

	taskUsageMutex    sync.Mutex
	taskUsageByPeriod map[taskUsagePeriod]taskUsageTotals
}

// OpenUsageLedger creates the usage tables when absent. httpClient reads
// OpenAI's daily token history for the accounts in dataDirectory.
func OpenUsageLedger(database *store.StoreDatabase, dataDirectory string, httpClient *http.Client) (*UsageLedger, error) {
	ledger := &UsageLedger{
		database: database, sqlDatabase: database.SQLDatabase(), dataDirectory: dataDirectory, httpClient: httpClient,
		lastLoggedProblem: map[string]string{}, taskUsageByPeriod: map[taskUsagePeriod]taskUsageTotals{},
	}
	if err := createOpenAIDailyTokenHistorySchema(ledger.sqlDatabase); err != nil {
		return nil, fmt.Errorf("create OpenAI daily token history schema: %w", err)
	}
	if err := createSessionUsageSchema(ledger.sqlDatabase); err != nil {
		return nil, fmt.Errorf("create native usage schema: %w", err)
	}
	return ledger, nil
}

// Run refreshes OpenAI's daily token history and the local session totals once a
// minute until ctx ends. A slow OpenAI read never delays the local totals.
func (ledger *UsageLedger) Run(ctx context.Context) {
	var refreshes sync.WaitGroup
	for _, refresh := range []func(context.Context){ledger.refreshOpenAIDailyTokenHistories, ledger.refreshTaskUsage} {
		refreshes.Add(1)
		go func() {
			defer refreshes.Done()
			ticker := time.NewTicker(usageRefreshInterval)
			defer ticker.Stop()
			for {
				refresh(ctx)
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	refreshes.Wait()
}

// ReadReportingPeriods returns Today, 7 Days and 30 Days with each account's
// OpenAI token total. When the local totals reconcile with those, each period
// also carries account costs and per-task tokens and costs. It never starts a
// network request.
func (ledger *UsageLedger) ReadReportingPeriods() ([]*menupb.ReportingPeriod, error) {
	periods, err := ledger.readProviderReportingPeriods()
	if err != nil {
		return nil, err
	}
	ledger.addTaskUsage(periods)
	return periods, nil
}
