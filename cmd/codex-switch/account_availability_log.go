package main

import (
	"fmt"
	"strings"

	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/routing"
)

// describeAccountAvailabilityChange returns the proxy log line for an account
// whose availability changed since the previous refresh, or "" when nothing an
// operator needs to know has changed. Allowance percentages and repeated
// failures of the same kind do not count as changes; the first observation
// after the proxy starts always does.
func describeAccountAvailabilityChange(previous, current routing.AccountRoutingFact, email string) string {
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
	case accountapi.AccountReady:
		state = "available"
	case accountapi.AccountLimited:
		state = "OpenAI reports its usage limit reached"
	case accountapi.AccountSignInRequired:
		state = fmt.Sprintf("signed out (%v); run: codex-switch login %s", current.AccountStateCause, current.AccountName)
	case accountapi.AccountCredentialsUnavailable:
		state = fmt.Sprintf("cannot read its saved login: %v", current.AccountStateCause)
	case accountapi.AccountUsageCheckFailed:
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
func allowanceReadFailing(fact routing.AccountRoutingFact) bool {
	return fact.AccountStateCause != nil && !fact.AllowanceSample.ObservedAt.IsZero() &&
		(fact.AccountState == accountapi.AccountReady || fact.AccountState == accountapi.AccountLimited)
}

func describeAllowanceWindows(sample accountapi.AllowanceSample) string {
	windows := make([]string, 0, len(sample.Windows))
	for _, window := range sample.Windows {
		windows = append(windows, fmt.Sprintf("%s %g%% used, %g%% free, resets %s",
			allowancePeriodLabel(window.DurationSeconds), window.UsedPercent, 100-window.UsedPercent,
			window.ResetsAt.Local().Format("Mon 2 Jan 15:04")))
	}
	return strings.Join(windows, "; ")
}
