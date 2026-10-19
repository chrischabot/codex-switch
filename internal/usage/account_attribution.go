package usage

import (
	"time"

	"github.com/joshp123/codex-switch/internal/store"
)

type allowanceOwnerReader interface {
	FindAccountForAllowanceWindow(int64, time.Time, time.Time) (store.ProviderAccountID, bool, error)
}

type nativeLocation struct {
	path   string
	offset int64
}
type allowanceIdentityKey struct{ seconds, reset int64 }

// attributeAccounts uses recorded serving identities or unique authenticated allowance windows.
func attributeAccounts(completions []reconciledCompletion, native []sessionUsageObservation, identities []store.AccountIdentity, windows allowanceOwnerReader) error {
	providerByName := make(map[store.AccountName]store.ProviderAccountID)
	for _, identity := range identities {
		providerByName[identity.AccountName] = store.ProviderAccountID(identity.ProviderAccountID)
	}
	responseAllowances := make(map[string]*sessionAllowance)
	locationAllowances := make(map[nativeLocation]*sessionAllowance)
	for _, observation := range native {
		if observation.Allowance == nil {
			continue
		}
		if observation.ResponseID != "" {
			responseAllowances[observation.ResponseID] = observation.Allowance
		}
		if observation.Usage != nil {
			locationAllowances[nativeLocation{observation.SourcePath, observation.SourceOffset}] = observation.Allowance
		}
	}
	owners := make(map[allowanceIdentityKey]store.ProviderAccountID)
	checked := make(map[allowanceIdentityKey]bool)
	for index := range completions {
		completion := &completions[index]
		// The proxy's own record of the serving account is the truth. Allowance
		// windows only attribute usage the proxy did not serve: accounts can share
		// a reset time (OpenAI resets several at once), so a window is a guess.
		completion.ProviderAccountID = providerByName[completion.AccountName]
		if completion.ProviderAccountID != "" {
			continue
		}
		allowance := responseAllowances[string(completion.ResponseID)]
		if source := completion.NativeSource; allowance == nil && source != nil {
			allowance = locationAllowances[nativeLocation{source.SourcePath, source.SourceOffset}]
		}
		if allowance == nil || (allowance.LimitID != "" && allowance.LimitID != "codex") {
			continue
		}
		for _, window := range []*sessionAllowanceWindow{allowance.Primary, allowance.Secondary} {
			if window == nil || window.WindowMinutes <= 0 || window.ResetsAt.IsZero() {
				continue
			}
			duration := window.WindowMinutes * 60
			if completion.At.Before(window.ResetsAt.Add(-time.Duration(duration)*time.Second)) || !completion.At.Before(window.ResetsAt) {
				continue
			}
			key := allowanceIdentityKey{duration, window.ResetsAt.Unix()}
			if !checked[key] {
				owner, found, err := windows.FindAccountForAllowanceWindow(duration, window.ResetsAt, completion.At)
				if err != nil {
					return err
				}
				checked[key] = true
				if found {
					owners[key] = owner
				}
			}
			owner := owners[key]
			if owner == "" {
				continue
			}
			completion.ProviderAccountID = owner
		}
	}
	return nil
}
