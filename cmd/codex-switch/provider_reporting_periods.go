package main

import (
	"time"

	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/menupb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func providerReportingPeriods(pool []accountapi.PoolAccount, histories map[string]accountapi.OpenAIDailyTokenHistory) ([]*menupb.ReportingPeriod, error) {
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
			total, err := histories[account.AccountID].TokensForReportedDays(definition.days)
			if err != nil {
				return nil, err
			}
			period.Accounts = append(period.Accounts, &menupb.AccountTotal{AccountKey: account.Name, Tokens: total})
		}
		periods = append(periods, period)
	}
	return periods, nil
}
