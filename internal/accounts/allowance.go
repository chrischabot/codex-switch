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
	UsedPercent     float64
	DurationSeconds int64
	ResetsAt        time.Time
}

type AllowanceSample struct {
	Route                 string
	AccountID             string
	Email                 string
	Plan                  string
	Allowed               bool
	LimitReached          bool
	ObservedAt            time.Time
	AvailableResetCredits *uint32
	Windows               []ReportedWindow
}
