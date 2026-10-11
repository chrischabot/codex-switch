// Package usage answers how many tokens each account and each task used, and
// what that work would cost at OpenAI's API prices.
//
// Account totals come from OpenAI's own per-account report, which counts every
// machine and ChatGPT Work on the web. Task totals come from what this Mac
// saw: local Codex and Pi session history joined with the responses the relay
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

	logMutex      sync.Mutex
	lastLoggedFor map[string]string

	periodsMutex sync.Mutex
	periods      []*menupb.ReportingPeriod
}

// OpenUsageLedger creates the usage tables when absent. httpClient reads
// OpenAI's daily token history for the accounts in dataDirectory.
func OpenUsageLedger(database *store.StoreDatabase, dataDirectory string, httpClient *http.Client) (*UsageLedger, error) {
	ledger := &UsageLedger{
		database: database, sqlDatabase: database.SQLDatabase(), dataDirectory: dataDirectory, httpClient: httpClient,
		lastLoggedFor: map[string]string{},
	}
	if err := createOpenAIDailyTokenHistorySchema(ledger.sqlDatabase); err != nil {
		return nil, fmt.Errorf("create OpenAI daily token history schema: %w", err)
	}
	if err := createSessionUsageSchema(ledger.sqlDatabase); err != nil {
		return nil, fmt.Errorf("create native usage schema: %w", err)
	}
	return ledger, nil
}

// Run refreshes OpenAI's per-account report and the reporting periods once a
// minute until ctx ends. A slow OpenAI read never delays the local totals.
func (ledger *UsageLedger) Run(ctx context.Context) {
	var refreshes sync.WaitGroup
	for _, refresh := range []func(context.Context){ledger.refreshOpenAIDailyTokenHistories, ledger.refreshReportingPeriods} {
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

// ReadReportingPeriods returns Today, 7 Days and 30 Days as of the last
// refresh: each account's tokens and API-equivalent cost, and each task's
// tokens and cost on this Mac. It never starts a network request.
func (ledger *UsageLedger) ReadReportingPeriods() []*menupb.ReportingPeriod {
	ledger.periodsMutex.Lock()
	defer ledger.periodsMutex.Unlock()
	return ledger.periods
}
