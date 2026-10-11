package routing

import (
	"time"

	"github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/store"
)

// AccountRoutingFact keeps provider eligibility and its matching credentials in one selection unit.
type AccountRoutingFact struct {
	AccountName       store.AccountName
	Credentials       accounts.Credentials
	AccountState      accounts.AccountState
	AccountStateCause error
	AllowanceSample   accounts.AllowanceSample
	RefreshedAt       time.Time
	// Nil means the catalogue has not been observed; an empty slice means no models.
	SupportedModelSlugs []string
	// ModelListCause is why the latest model-list read failed; nil after a successful read.
	ModelListCause error
}

// AccountRoutingFactsSource isolates selection from account-home discovery.
type AccountRoutingFactsSource interface {
	ReadAccountRoutingFacts() ([]AccountRoutingFact, error)
	RequestAccountRoutingFactsRefresh()
	ExcludeUnsupportedAccountModel(accountName store.AccountName, modelSlug string)
	RefreshAccountCredentialsAfterUnauthorized(accountName store.AccountName, rejected accounts.Credentials) error
}
