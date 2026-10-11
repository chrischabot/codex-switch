package accounts

import (
	"math"
	"time"
)

// AllowanceWindowKind identifies an OpenAI allowance by its provider-reported duration.
// The primary and secondary positions are not stable product identities.
type AllowanceWindowKind string

const (
	FiveHours AllowanceWindowKind = "5-hour"
	Weekly    AllowanceWindowKind = "weekly"
)

func (k AllowanceWindowKind) Duration() time.Duration {
	switch k {
	case FiveHours:
		return 5 * time.Hour
	case Weekly:
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

// AllowanceWindow is one provider-reported allowance window that is current
// at the time it was analysed.
type AllowanceWindow struct {
	FreePercent float64
	ResetsAt    time.Time
}

// AnalyseAllowance returns the current window of one kind from a provider
// sample, without inferring any future usage. It reports false when the
// account state, the sample or the window gives no trustworthy reading.
func AnalyseAllowance(sample AllowanceSample, state AccountState, asOf time.Time, kind AllowanceWindowKind) (AllowanceWindow, bool) {
	if state != AccountReady && state != AccountLimited {
		return AllowanceWindow{}, false
	}
	wantDuration := kind.Duration()
	if wantDuration <= 0 || sample.ObservedAt.IsZero() || asOf.IsZero() {
		return AllowanceWindow{}, false
	}

	var matched *ReportedWindow
	for index := range sample.Windows {
		window := sample.Windows[index]
		if time.Duration(window.DurationSeconds)*time.Second != wantDuration {
			continue
		}
		if matched != nil {
			return AllowanceWindow{}, false
		}
		matched = &window
	}
	if matched == nil || matched.ResetsAt.IsZero() || math.IsNaN(matched.UsedPercent) ||
		math.IsInf(matched.UsedPercent, 0) || matched.UsedPercent < 0 || matched.UsedPercent > 100 {
		return AllowanceWindow{}, false
	}

	observedAt := sample.ObservedAt.UTC()
	asOf = asOf.UTC()
	resetsAt := matched.ResetsAt.UTC()
	if observedAt.Before(resetsAt.Add(-wantDuration)) || asOf.Before(observedAt) ||
		!observedAt.Before(resetsAt) || !asOf.Before(resetsAt) {
		return AllowanceWindow{}, false
	}
	return AllowanceWindow{FreePercent: 100 - matched.UsedPercent, ResetsAt: resetsAt}, true
}
