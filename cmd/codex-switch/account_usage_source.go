package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/menupb"
	"github.com/josh/codex-switch/internal/store"
)

const accountUsagePollInterval = time.Minute

// poolAccountUsageSource reads whole-account profiles independently of routing and credential refresh.
type poolAccountUsageSource struct {
	dataDirectory string
	httpClient    *http.Client
	database      *store.StoreDatabase

	problemMutex      sync.Mutex
	lastLoggedProblem map[string]string
}

func newPoolAccountUsageSource(dataDirectory string, httpClient *http.Client, database *store.StoreDatabase) *poolAccountUsageSource {
	return &poolAccountUsageSource{dataDirectory: dataDirectory, httpClient: httpClient, database: database, lastLoggedProblem: map[string]string{}}
}

func (source *poolAccountUsageSource) Run(ctx context.Context) {
	ticker := time.NewTicker(accountUsagePollInterval)
	defer ticker.Stop()
	for {
		source.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (source *poolAccountUsageSource) refresh(ctx context.Context) {
	pool, err := accountapi.ReadPool(source.dataDirectory)
	if err != nil {
		source.logProblemOnce("", fmt.Sprintf("could not read the account pool: %v", err))
		return
	}
	source.logProblemOnce("", "")
	var workers sync.WaitGroup
	for _, account := range pool {
		workers.Add(1)
		go func(account accountapi.PoolAccount) {
			defer workers.Done()
			requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			profileSource := accountapi.NewOpenAIDailyTokenSource(filepath.Join(source.dataDirectory, account.Name), source.httpClient)
			history, err := profileSource.Read(requestContext)
			if err != nil {
				if ctx.Err() == nil {
					source.logProblemOnce(account.Name, fmt.Sprintf("%s: could not read OpenAI's daily token history: %v", account.Name, err))
				}
				return
			}
			if history.AccountID != account.AccountID {
				source.logProblemOnce(account.Name, fmt.Sprintf("%s: OpenAI's daily token history belongs to a different account than its saved login", account.Name))
				return
			}
			if _, err = source.database.RecordProviderTokenHistory(history); err != nil {
				source.logProblemOnce(account.Name, fmt.Sprintf("%s: could not save OpenAI's daily token history: %v", account.Name, err))
				return
			}
			source.logProblemOnce(account.Name, "")
		}(account)
	}
	workers.Wait()
}

// ReadReportingPeriods uses current pool membership and never starts a network request.
func (source *poolAccountUsageSource) ReadReportingPeriods() ([]*menupb.ReportingPeriod, error) {
	pool, err := accountapi.ReadPool(source.dataDirectory)
	if err != nil {
		return nil, err
	}
	histories, err := source.database.ReadProviderTokenHistories()
	if err != nil {
		return nil, err
	}
	return providerReportingPeriods(pool, histories)
}

// logProblemOnce logs a problem reading daily token history when it differs from
// the last one logged for the account, so a lasting outage is one line, not one a
// minute. An empty problem records recovery silently.
func (source *poolAccountUsageSource) logProblemOnce(accountName, problem string) {
	source.problemMutex.Lock()
	defer source.problemMutex.Unlock()
	if source.lastLoggedProblem[accountName] == problem {
		return
	}
	source.lastLoggedProblem[accountName] = problem
	if problem != "" {
		log.Print(problem)
	}
}
