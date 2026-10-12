// Package usage answers how many tokens each account and each task used, and
// what that work would cost at the provider's API prices.
//
// Account totals come from OpenAI's own per-account report, which counts every
// machine and ChatGPT Work on the web. Task totals come from what this Mac
// saw: local Codex and Pi session history joined with the responses the relay
// recorded. Claude account and task totals use the same served responses.
// It keeps its own tables in the store's database.
package usage

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/joshp123/codex-switch/internal/store"
)

const usageRefreshInterval = time.Minute

type UsageLedger struct {
	database      *store.StoreDatabase
	sqlDatabase   *sql.DB
	dataDirectory string
	httpClient    *http.Client

	logMutex      sync.Mutex
	lastLoggedFor map[string]string
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

// Run refreshes OpenAI's per-account report once a minute until ctx ends,
// so a slow OpenAI read never delays the local totals.
func (ledger *UsageLedger) Run(ctx context.Context) {
	ticker := time.NewTicker(usageRefreshInterval)
	defer ticker.Stop()
	for {
		ledger.refreshOpenAIDailyTokenHistories(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
