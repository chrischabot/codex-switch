package routing

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/store"
)

func TestUnsupportedModelRetryEntersCapacityWait(t *testing.T) {
	for _, test := range []struct {
		name                        string
		rejectedModels, otherModels []string
		retry                       bool
	}{
		{"observed exclusion with limited supported account", []string{"luna"}, []string{"astra"}, true},
		{"explicit rejection supersedes stale catalogue", []string{"astra"}, []string{"astra"}, true},
		{"unknown rejected catalogue", nil, []string{"astra"}, false},
		{"no account supports model", []string{"luna"}, []string{"luna"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, err := store.OpenStoreDatabase(filepath.Join(t.TempDir(), "routing.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.CloseStoreDatabase()
			facts := capacityWaitFacts{
				{AccountName: "free", Credentials: accounts.Credentials{AccessToken: "test", AccountID: "free"}, AccountState: accounts.AccountReady, SupportedModelSlugs: test.rejectedModels},
				{AccountName: "pro", Credentials: accounts.Credentials{AccessToken: "test", AccountID: "pro"}, AccountState: accounts.AccountLimited, SupportedModelSlugs: test.otherModels},
			}
			router := CreateTaskRouter(database, facts)
			if got := router.ReportUnsupportedModel("task", "free", "astra"); got != test.retry {
				t.Fatalf("retry = %v, want %v", got, test.retry)
			}
			if test.retry {
				_, err := router.RouteForTask("task", store.ModelTurnWorkKind, "astra")
				if !errors.Is(err, ErrNoAvailableAccount) {
					t.Fatalf("next route = %v, want capacity wait", err)
				}
			}
		})
	}
}

type capacityWaitFacts []accounts.AccountRoutingFact

func (facts capacityWaitFacts) ReadAccountRoutingFacts() ([]accounts.AccountRoutingFact, error) {
	return facts, nil
}
func (facts capacityWaitFacts) RequestAccountRoutingFactsRefresh() {}
func (facts capacityWaitFacts) RefreshAccountCredentialsAfterUnauthorized(store.AccountName, accounts.Credentials) error {
	return nil
}

func (facts capacityWaitFacts) ExcludeUnsupportedAccountModel(name store.AccountName, modelSlug string) {
	for index := range facts {
		if facts[index].AccountName == name {
			facts[index].SupportedModelSlugs = slices.DeleteFunc(slices.Clone(facts[index].SupportedModelSlugs), func(slug string) bool { return slug == modelSlug })
		}
	}
}
