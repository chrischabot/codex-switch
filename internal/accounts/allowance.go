package accounts

import "time"

type AccountState string

const (
	AccountReady                  AccountState = "ready"
	AccountUsageCheckPending      AccountState = "usage-check-pending"
	AccountSignInRequired         AccountState = "sign-in-required"
	AccountCredentialsUnavailable AccountState = "credentials-unavailable"
	AccountLimited                AccountState = "limited"
	AccountUsageCheckFailed       AccountState = "usage-check-failed"
)

func (s AccountState) Eligible() bool {
	return s == AccountReady || s == AccountUsageCheckPending || s == AccountUsageCheckFailed
}

type ReportedWindow struct {
	UsedPercent     float64       `json:"used_percent"`
	Duration        time.Duration `json:"-"`
	DurationSeconds int64         `json:"duration_seconds"`
	ResetsAt        time.Time     `json:"resets_at"`
}

type AllowanceSample struct {
	Route                 string           `json:"route"`
	AccountID             string           `json:"account_id,omitempty"`
	Email                 string           `json:"email,omitempty"`
	Plan                  string           `json:"plan,omitempty"`
	Allowed               bool             `json:"allowed"`
	LimitReached          bool             `json:"limit_reached"`
	ObservedAt            time.Time        `json:"observed_at"`
	AvailableResetCredits *uint32          `json:"available_reset_credits,omitempty"`
	Windows               []ReportedWindow `json:"windows"`
}
