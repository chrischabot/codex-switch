package routing

import (
	"github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/store"
)

// AccountRoutingFactsSource isolates selection from account-home discovery.
type AccountRoutingFactsSource interface {
	ReadAccountRoutingFacts() ([]accounts.AccountRoutingFact, error)
	RequestAccountRoutingFactsRefresh()
	ExcludeUnsupportedAccountModel(accountName store.AccountName, modelSlug string)
	RefreshAccountCredentialsAfterUnauthorized(accountName store.AccountName, rejected accounts.Credentials) error
}
