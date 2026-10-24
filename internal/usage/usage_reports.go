package usage

import (
	"context"
	"time"

	"github.com/joshp123/codex-switch/internal/accounts"
	"github.com/joshp123/codex-switch/internal/menupb"
)

func (ledger *UsageLedger) readUsageReports(ctx context.Context, accountKeys []string, now time.Time) ([]*menupb.ReportingPeriod, []*menupb.AccountDay, *menupb.ProxyActivity, error) {
	pool, err := accounts.ReadPool(ledger.dataDirectory)
	if err != nil {
		return nil, nil, nil, err
	}
	credits, err := ledger.readOpenAIModelDayCredits()
	if err != nil {
		return nil, nil, nil, err
	}
	standardStart, err := oldestCreditDate(credits)
	if err != nil {
		return nil, nil, nil, err
	}
	state, err := ledger.refreshUsageFold(ctx, now, pool, standardStart)
	if err != nil {
		return nil, nil, nil, err
	}
	metadata, err := ledger.foldMetadata(state)
	if err != nil {
		return nil, nil, nil, err
	}
	histories, err := ledger.readOpenAIDailyTokenHistories()
	if err != nil {
		return nil, nil, nil, err
	}
	requestsStarted, counts, err := ledger.database.ReadModelRequestCounts()
	if err != nil {
		return nil, nil, nil, err
	}
	cohorts := state.reportingCohorts()
	dailyUsage := ledger.accountDailyUsageFromHistories(pool, cohorts, credits, state.standardCost, histories)
	periods, days, err := reportUsagePeriods(state.today, pool, cohorts, metadata, state.turnOwnerTask, dailyUsage)
	if err != nil {
		return nil, nil, nil, err
	}
	activity, err := routedWorkWithRequestCounts(state, metadata, accountKeys, now, requestsStarted, counts)
	if err != nil {
		return nil, nil, nil, err
	}
	return periods, days, activity, nil
}
