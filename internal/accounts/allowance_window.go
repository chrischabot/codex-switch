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

type UnknownReason string

const (
	UnknownWindowNotReported      UnknownReason = "window-not-reported"
	UnknownInvalidWindow          UnknownReason = "invalid-window"
	UnknownWindowNotStarted       UnknownReason = "window-not-started"
	UnknownWindowAlreadyReset     UnknownReason = "window-already-reset"
	UnknownUsageCheckPending      UnknownReason = "usage-check-pending"
	UnknownSignInRequired         UnknownReason = "sign-in-required"
	UnknownCredentialsUnavailable UnknownReason = "credentials-unavailable"
	UnknownUsageCheckFailed       UnknownReason = "usage-check-failed"
	UnknownAccountState           UnknownReason = "account-state-unknown"
)

// AllowancePeriod is one complete provider allowance window. Token and cost events
// use this same half-open boundary so an event at the reset belongs to the next
// window.
type AllowancePeriod struct {
	StartedAt time.Time `json:"started_at"`
	ResetsAt  time.Time `json:"resets_at"`
}

func (p AllowancePeriod) Contains(at time.Time) bool {
	return !at.IsZero() && !p.StartedAt.IsZero() && !p.ResetsAt.IsZero() &&
		p.StartedAt.Before(p.ResetsAt) && !at.Before(p.StartedAt) && at.Before(p.ResetsAt)
}

type AllowanceWindow struct {
	Kind            AllowanceWindowKind `json:"kind"`
	DurationSeconds int64               `json:"duration_seconds"`
	Period          AllowancePeriod     `json:"period"`
	ObservedAt      time.Time           `json:"observed_at"`
	UsedPercent     float64             `json:"used_percent"`
	FreePercent     float64             `json:"free_percent"`
}

type AllowanceResult struct {
	Kind          AllowanceWindowKind `json:"kind"`
	Window        *AllowanceWindow    `json:"window,omitempty"`
	UnknownReason UnknownReason       `json:"unknown_reason,omitempty"`
}

func (r AllowanceResult) Known() bool { return r.UnknownReason == "" && r.Window != nil }

type AllowanceAnalysisInput struct {
	Sample AllowanceSample
	State  AccountState
	AsOf   time.Time
}

// AnalyseAllowance validates one provider-reported allowance window without
// inferring any future usage or historical account ownership.
func AnalyseAllowance(input AllowanceAnalysisInput, kind AllowanceWindowKind) AllowanceResult {
	sample := input.Sample
	result := AllowanceResult{
		Kind: kind,
	}
	switch input.State {
	case AccountReady, AccountLimited:
	case AccountUsageCheckPending:
		result.UnknownReason = UnknownUsageCheckPending
		return result
	case AccountSignInRequired:
		result.UnknownReason = UnknownSignInRequired
		return result
	case AccountCredentialsUnavailable:
		result.UnknownReason = UnknownCredentialsUnavailable
		return result
	case AccountUsageCheckFailed:
		result.UnknownReason = UnknownUsageCheckFailed
		return result
	default:
		result.UnknownReason = UnknownAccountState
		return result
	}
	wantDuration := kind.Duration()
	if wantDuration <= 0 || sample.ObservedAt.IsZero() || input.AsOf.IsZero() {
		result.UnknownReason = UnknownInvalidWindow
		return result
	}

	var matched *ReportedWindow
	invalidWindow := false
	for index := range sample.Windows {
		window := sample.Windows[index]
		duration, valid := providerDuration(window)
		if !valid {
			invalidWindow = true
			continue
		}
		if duration != wantDuration {
			continue
		}
		if matched != nil {
			result.UnknownReason = UnknownInvalidWindow
			return result
		}
		matched = &window
	}
	if matched == nil {
		if invalidWindow {
			result.UnknownReason = UnknownInvalidWindow
		} else {
			result.UnknownReason = UnknownWindowNotReported
		}
		return result
	}
	if matched.ResetsAt.IsZero() || math.IsNaN(matched.UsedPercent) || math.IsInf(matched.UsedPercent, 0) ||
		matched.UsedPercent < 0 || matched.UsedPercent > 100 {
		result.UnknownReason = UnknownInvalidWindow
		return result
	}

	observedAt := sample.ObservedAt.UTC()
	asOf := input.AsOf.UTC()
	resetsAt := matched.ResetsAt.UTC()
	startedAt := resetsAt.Add(-wantDuration)
	if observedAt.Before(startedAt) {
		result.UnknownReason = UnknownWindowNotStarted
		return result
	}
	if asOf.Before(observedAt) {
		result.UnknownReason = UnknownInvalidWindow
		return result
	}
	if !observedAt.Before(resetsAt) || !asOf.Before(resetsAt) {
		result.UnknownReason = UnknownWindowAlreadyReset
		return result
	}
	window := &AllowanceWindow{
		Kind: kind, DurationSeconds: int64(wantDuration.Seconds()),
		Period: AllowancePeriod{StartedAt: startedAt, ResetsAt: resetsAt}, ObservedAt: observedAt,
		UsedPercent: matched.UsedPercent, FreePercent: 100 - matched.UsedPercent,
	}
	result.Window = window
	return result
}

func providerDuration(window ReportedWindow) (time.Duration, bool) {
	secondsDuration := time.Duration(window.DurationSeconds) * time.Second
	if window.Duration != 0 && secondsDuration != 0 && window.Duration != secondsDuration {
		return 0, false
	}
	duration := window.Duration
	if duration == 0 {
		duration = secondsDuration
	}
	return duration, duration > 0
}
