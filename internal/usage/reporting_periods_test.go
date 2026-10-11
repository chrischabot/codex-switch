package usage

import (
	"testing"
	"time"

	"github.com/josh/codex-switch/internal/accounts"
)

func TestReportedTokensIncludeThePublishedDay(t *testing.T) {
	reportingDate := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	history := accounts.OpenAIDailyTokenHistory{ReportingDate: reportingDate, Buckets: []accounts.OpenAIDailyTokenBucket{
		{StartDate: "2026-07-02", Tokens: 2},
		{StartDate: "2026-07-03", Tokens: 3},
		{StartDate: "2026-07-26", Tokens: 26},
		{StartDate: "2026-07-31", Tokens: 31},
		{StartDate: "2026-08-01", Tokens: 1000},
	}}
	for days, want := range map[int]int64{1: 1000, 7: 1057, 30: 1060} {
		if got := tokensForReportedDays(history, days); got != want {
			t.Fatalf("%d reported days = %d; want %d", days, got, want)
		}
	}
	withoutActiveDay := accounts.OpenAIDailyTokenHistory{ReportingDate: reportingDate, Buckets: []accounts.OpenAIDailyTokenBucket{
		{StartDate: "2026-07-31", Tokens: 9},
	}}
	if got := tokensForReportedDays(withoutActiveDay, 1); got != 0 {
		t.Fatalf("reported day without activity = %d", got)
	}
	if got := tokensForReportedDays(withoutActiveDay, 7); got != 9 {
		t.Fatalf("reported week without an active-day bucket = %d", got)
	}
}
