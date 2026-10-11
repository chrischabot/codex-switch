package usage

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/josh/codex-switch/internal/accounts"
)

// refreshOpenAIDailyTokenHistories reads every pool account's daily token
// history from OpenAI, independently of routing and credential refresh.
func (ledger *UsageLedger) refreshOpenAIDailyTokenHistories(ctx context.Context) {
	pool, err := accounts.ReadPool(ledger.dataDirectory)
	if err != nil {
		ledger.logProblemOnce("", fmt.Sprintf("could not read the account pool: %v", err))
		return
	}
	ledger.logProblemOnce("", "")
	var workers sync.WaitGroup
	for _, account := range pool {
		workers.Add(1)
		go func(account accounts.PoolAccount) {
			defer workers.Done()
			requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			profileSource := accounts.NewOpenAIDailyTokenSource(filepath.Join(ledger.dataDirectory, account.Name), ledger.httpClient)
			history, err := profileSource.Read(requestContext)
			if err != nil {
				if ctx.Err() == nil {
					ledger.logProblemOnce(account.Name, fmt.Sprintf("%s: could not read OpenAI's daily token history: %v", account.Name, err))
				}
				return
			}
			if history.AccountID != account.AccountID {
				ledger.logProblemOnce(account.Name, fmt.Sprintf("%s: OpenAI's daily token history belongs to a different account than its saved login", account.Name))
				return
			}
			if _, err = ledger.saveOpenAIDailyTokenHistory(history); err != nil {
				ledger.logProblemOnce(account.Name, fmt.Sprintf("%s: could not save OpenAI's daily token history: %v", account.Name, err))
				return
			}
			ledger.logProblemOnce(account.Name, "")
		}(account)
	}
	workers.Wait()
}

// logProblemOnce logs a problem reading daily token history when it differs from
// the last one logged for the account, so a lasting outage is one line, not one a
// minute. An empty problem records recovery silently.
func (ledger *UsageLedger) logProblemOnce(accountName, problem string) {
	ledger.problemMutex.Lock()
	defer ledger.problemMutex.Unlock()
	if ledger.lastLoggedProblem[accountName] == problem {
		return
	}
	ledger.lastLoggedProblem[accountName] = problem
	if problem != "" {
		log.Print(problem)
	}
}
