package usage

import (
	"time"

	"github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/menupb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// readProviderReportingPeriods uses current pool membership and never starts a network request.
func (ledger *UsageLedger) readProviderReportingPeriods() ([]*menupb.ReportingPeriod, error) {
	pool, err := accounts.ReadPool(ledger.dataDirectory)
	if err != nil {
		return nil, err
	}
	histories, err := ledger.readOpenAIDailyTokenHistories()
	if err != nil {
		return nil, err
	}
	return providerReportingPeriods(pool, histories)
}

// providerReportingPeriods totals each account's OpenAI daily tokens for Today, 7 Days and 30 Days,
// ending on the reporting date every account shares.
func providerReportingPeriods(pool []accounts.PoolAccount, histories map[string]accounts.OpenAIDailyTokenHistory) ([]*menupb.ReportingPeriod, error) {
	if len(pool) == 0 {
		return nil, nil
	}
	var reportingDate time.Time
	for _, account := range pool {
		history, available := histories[account.AccountID]
		if !available || history.AccountID != account.AccountID || history.ReportingDate.IsZero() {
			return nil, nil
		}
		if reportingDate.IsZero() {
			reportingDate = history.ReportingDate
		} else if !reportingDate.Equal(history.ReportingDate) {
			return nil, nil
		}
	}
	periods := make([]*menupb.ReportingPeriod, 0, 3)
	for _, definition := range []struct {
		kind menupb.ReportingPeriodKind
		days int
	}{
		{menupb.ReportingPeriodKind_REPORTING_PERIOD_TODAY, 1},
		{menupb.ReportingPeriodKind_REPORTING_PERIOD_SEVEN_DAYS, 7},
		{menupb.ReportingPeriodKind_REPORTING_PERIOD_THIRTY_DAYS, 30},
	} {
		period := &menupb.ReportingPeriod{
			Kind:      definition.kind,
			StartedAt: timestamppb.New(reportingDate.AddDate(0, 0, 1-definition.days)),
			EndedAt:   timestamppb.New(reportingDate.AddDate(0, 0, 1)),
		}
		for _, account := range pool {
			total := tokensForReportedDays(histories[account.AccountID], definition.days)
			period.Accounts = append(period.Accounts, &menupb.AccountTotal{AccountKey: account.Name, Tokens: total})
		}
		periods = append(periods, period)
	}
	return periods, nil
}

// tokensForReportedDays includes OpenAI's reporting date and the preceding calendar days.
func tokensForReportedDays(history accounts.OpenAIDailyTokenHistory, numberOfDays int) int64 {
	firstDate := history.ReportingDate.AddDate(0, 0, 1-numberOfDays).Format(time.DateOnly)
	lastDate := history.ReportingDate.Format(time.DateOnly)
	var total int64
	for _, bucket := range history.Buckets {
		if bucket.StartDate >= firstDate && bucket.StartDate <= lastDate {
			total += bucket.Tokens
		}
	}
	return total
}
