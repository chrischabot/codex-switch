package accounts

import (
	"fmt"
	"strings"
	"time"
)

// describeAccountAvailabilityChange returns the proxy log line for an account
// whose availability changed since the previous refresh, or "" when nothing an
// operator needs to know has changed. Allowance percentages and repeated
// failures of the same kind do not count as changes; the first observation
// after the proxy starts always does.
func describeAccountAvailabilityChange(previous, current AccountRoutingFact, email string) string {
	firstObservation := previous.AccountName == ""
	stateChanged := previous.AccountState != current.AccountState
	modelListChanged := (previous.SupportedModelSlugs == nil) != (current.SupportedModelSlugs == nil)
	allowanceReadChanged := allowanceReadFailing(previous) != allowanceReadFailing(current)
	modelListReadChanged := (previous.ModelListCause == nil) != (current.ModelListCause == nil)
	if !firstObservation && !stateChanged && !modelListChanged && !allowanceReadChanged && !modelListReadChanged {
		return ""
	}
	identity := string(current.AccountName)
	details := []string{}
	for _, detail := range []string{email, current.AllowanceSample.Plan} {
		if detail != "" {
			details = append(details, detail)
		}
	}
	if len(details) > 0 {
		identity += " (" + strings.Join(details, ", ") + ")"
	}

	var state string
	switch current.AccountState {
	case AccountReady:
		state = "available"
	case AccountLimited:
		state = "OpenAI reports its usage limit reached"
	case AccountSignInRequired:
		state = fmt.Sprintf("signed out (%v); run: codex-switch login %s", current.AccountStateCause, current.AccountName)
	case AccountCredentialsUnavailable:
		state = fmt.Sprintf("cannot read its saved login: %v", current.AccountStateCause)
	case AccountUsageCheckFailed:
		state = fmt.Sprintf("available, but its allowance could not be read: %v", current.AccountStateCause)
	default:
		state = "state " + string(current.AccountState)
	}
	line := identity + ": " + state
	if allowanceReadFailing(current) {
		line += fmt.Sprintf(" (its allowance could not be read again, so routing uses the reading from %s: %v)",
			current.AllowanceSample.ObservedAt.Local().Format("Mon 2 Jan 15:04"), current.AccountStateCause)
	}
	if allowance := describeAllowanceWindows(current.AllowanceSample); allowance != "" {
		line += "; " + allowance
	}
	switch {
	case current.ModelListCause != nil && current.SupportedModelSlugs == nil:
		line += fmt.Sprintf("; its model list could not be read, so model requests skip it: %v", current.ModelListCause)
	case current.ModelListCause != nil:
		line += fmt.Sprintf("; its model list could not be read again, so routing uses the earlier list: %v", current.ModelListCause)
	}
	return line
}

// allowanceReadFailing reports an account whose latest allowance read failed
// while routing keeps using an earlier reading.
func allowanceReadFailing(fact AccountRoutingFact) bool {
	return fact.AccountStateCause != nil && !fact.AllowanceSample.ObservedAt.IsZero() &&
		(fact.AccountState == AccountReady || fact.AccountState == AccountLimited)
}

func describeAllowanceWindows(sample AllowanceSample) string {
	windows := make([]string, 0, len(sample.Windows))
	for _, window := range sample.Windows {
		windows = append(windows, fmt.Sprintf("%s %g%% used, %g%% free, resets %s",
			AllowancePeriodLabel(window.DurationSeconds), window.UsedPercent, 100-window.UsedPercent,
			window.ResetsAt.Local().Format("Mon 2 Jan 15:04")))
	}
	return strings.Join(windows, "; ")
}

// AllowancePeriodLabel names an allowance window the way the proxy log and the
// status command show it.
func AllowancePeriodLabel(seconds int64) string {
	switch seconds {
	case 5 * 3600:
		return "5-hour"
	case 7 * 86400:
		return "weekly"
	}
	if seconds >= 86400 && seconds%86400 == 0 {
		return fmt.Sprintf("%d days", seconds/86400)
	}
	if seconds >= 3600 && seconds%3600 == 0 {
		return fmt.Sprintf("%d hours", seconds/3600)
	}
	return (time.Duration(seconds) * time.Second).String()
}
