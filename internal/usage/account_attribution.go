package usage

import (
	"time"

	"github.com/joshp123/codex-switch/internal/store"
)

type allowanceIdentityKey struct{ seconds, reset int64 }

// attributeCompletion prefers the serving account and otherwise uses only
// authenticated allowance windows covering the completion's timestamp.
func attributeCompletion(completion *reconciledCompletion, allowance *sessionAllowance, providerByName map[store.AccountName]store.ProviderAccountID, owners map[allowanceIdentityKey]store.ProviderAccountID) {
	completion.ProviderAccountID = providerByName[completion.AccountName]
	if completion.ProviderAccountID != "" {
		return
	}
	if allowance == nil || (allowance.LimitID != "" && allowance.LimitID != "codex") {
		return
	}
	for _, window := range []*sessionAllowanceWindow{allowance.Primary, allowance.Secondary} {
		if window == nil || window.WindowMinutes <= 0 || window.ResetsAt.IsZero() {
			continue
		}
		duration := window.WindowMinutes * 60
		if completion.At.Before(window.ResetsAt.Add(-time.Duration(duration)*time.Second)) || !completion.At.Before(window.ResetsAt) {
			continue
		}
		if owner := owners[allowanceIdentityKey{duration, window.ResetsAt.Unix()}]; owner != "" {
			completion.ProviderAccountID = owner
		}
	}
}
