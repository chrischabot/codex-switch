package store

import (
	"errors"
	"time"
)

type OpenAIDailyTokenBucket struct {
	StartDate string
	Tokens    int64
}

// OpenAIDailyTokenHistory is an account-scoped OpenAI profile snapshot.
// OpenAI supplies the reporting date independently of the local read time.
type OpenAIDailyTokenHistory struct {
	AccountID      string
	ReadStartedAt  time.Time
	ReportingDate  time.Time
	GeneratedAt    time.Time
	LifetimeTokens int64
	Buckets        []OpenAIDailyTokenBucket
}

// TokensForReportedDays includes the provider's reporting date and the preceding calendar days.
func (h OpenAIDailyTokenHistory) TokensForReportedDays(numberOfDays int) (int64, error) {
	if numberOfDays < 1 || h.ReportingDate.IsZero() {
		return 0, errors.New("OpenAI reporting day range is invalid")
	}
	firstDate := h.ReportingDate.AddDate(0, 0, 1-numberOfDays).Format(time.DateOnly)
	lastDate := h.ReportingDate.Format(time.DateOnly)
	var total int64
	for _, bucket := range h.Buckets {
		if bucket.StartDate >= firstDate && bucket.StartDate <= lastDate {
			total += bucket.Tokens
		}
	}
	return total, nil
}
