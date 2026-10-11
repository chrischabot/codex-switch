package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"sync"
	"time"

	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/codexstate"
	"github.com/josh/codex-switch/internal/routing"
	"github.com/josh/codex-switch/internal/store"
)

// Healthy accounts refresh once a minute; waiting work requests a refresh at
// most every five seconds. Network work never runs on the routing path.
const allowanceFactFreshness = time.Minute

var errWhamUsageUnauthorized = errors.New("wham/usage rejected account credentials")

// poolAccountRoutingFactsSource adapts the on-disk account pool and the
// provider observations to routing. One process-owned loop discovers accounts
// and refreshes them independently, with one in-flight read per account.
type poolAccountRoutingFactsSource struct {
	dataDirectory   string
	usageURL        string
	modelsURL       string
	codexExecutable string
	httpClient      *http.Client
	storeDatabase   *store.StoreDatabase

	mutex            sync.Mutex
	cachedFacts      map[store.AccountName]routing.AccountRoutingFact
	credentialStores map[store.AccountName]*accountapi.CredentialStore
	lastAttempt      map[store.AccountName]time.Time
	refreshing       map[store.AccountName]bool
	refreshRequested chan struct{}
	firstReadDone    chan struct{}
	firstReadOnce    sync.Once
}

func newPoolAccountRoutingFactsSource(dataDirectory string, upstreamOrigin *url.URL, httpClient *http.Client, storeDatabase *store.StoreDatabase) *poolAccountRoutingFactsSource {
	usageURL := upstreamOrigin.ResolveReference(&url.URL{Path: "/backend-api/wham/usage"}).String()
	return &poolAccountRoutingFactsSource{
		dataDirectory:    dataDirectory,
		usageURL:         usageURL,
		modelsURL:        upstreamOrigin.ResolveReference(&url.URL{Path: "/backend-api/codex/models"}).String(),
		codexExecutable:  codexstate.FindCodexExecutable(),
		httpClient:       httpClient,
		storeDatabase:    storeDatabase,
		cachedFacts:      map[store.AccountName]routing.AccountRoutingFact{},
		credentialStores: map[store.AccountName]*accountapi.CredentialStore{},
		lastAttempt:      map[store.AccountName]time.Time{},
		refreshing:       map[store.AccountName]bool{},
		refreshRequested: make(chan struct{}, 1),
		firstReadDone:    make(chan struct{}),
	}
}

// WaitForFirstAccountRead returns once every account has been read once,
// successfully or not.
func (source *poolAccountRoutingFactsSource) WaitForFirstAccountRead() {
	<-source.firstReadDone
}

// Run owns account discovery and refresh until the serving process has drained.
func (source *poolAccountRoutingFactsSource) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var workers sync.WaitGroup
	defer workers.Wait()
	refreshRequested := true
	firstPass := true
	var firstRead sync.WaitGroup
	for {
		pool, err := accountapi.ReadPool(source.dataDirectory)
		source.mutex.Lock()
		if err == nil {
			for _, account := range pool {
				name := store.AccountName(account.Name)
				interval := allowanceFactFreshness
				if refreshRequested {
					interval = 5 * time.Second
				}
				if source.refreshing[name] || time.Since(source.lastAttempt[name]) < interval {
					continue
				}
				source.refreshing[name] = true
				source.lastAttempt[name] = time.Now()
				workers.Add(1)
				if firstPass {
					firstRead.Add(1)
				}
				previous := source.cachedFacts[name]
				go func(account accountapi.PoolAccount, partOfFirstRead bool) {
					defer workers.Done()
					if partOfFirstRead {
						defer firstRead.Done()
					}
					fact := source.fetchAccountRoutingFact(ctx, account)
					source.mutex.Lock()
					latest := source.cachedFacts[fact.AccountName]
					if latest.Credentials != previous.Credentials {
						fact.Credentials = latest.Credentials
					}
					if !slices.Equal(latest.SupportedModelSlugs, previous.SupportedModelSlugs) {
						fact.SupportedModelSlugs = latest.SupportedModelSlugs
					}
					if latest.AccountState == accountapi.AccountSignInRequired && previous.AccountState != accountapi.AccountSignInRequired && fact.Credentials == latest.Credentials {
						fact.AccountState = latest.AccountState
						fact.AccountStateCause = latest.AccountStateCause
					}
					availabilityChange := describeAccountAvailabilityChange(latest, fact, account.Email)
					source.cachedFacts[fact.AccountName] = fact
					source.refreshing[fact.AccountName] = false
					source.mutex.Unlock()
					if availabilityChange != "" && ctx.Err() == nil {
						log.Print(availabilityChange)
					}
				}(account, firstPass)
			}
		}
		source.mutex.Unlock()
		if err != nil {
			log.Printf("could not read the account pool: %v", err)
		}
		if firstPass {
			firstPass = false
			go func() {
				firstRead.Wait()
				source.firstReadOnce.Do(func() { close(source.firstReadDone) })
			}()
		}
		refreshRequested = false
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-source.refreshRequested:
			refreshRequested = true
		}
	}
}

// ReadAccountRoutingFacts joins cached provider facts to current pool membership.
// Removed or replaced accounts cannot admit new work before the refresh loop catches up.
func (source *poolAccountRoutingFactsSource) ReadAccountRoutingFacts() ([]routing.AccountRoutingFact, error) {
	pool, err := accountapi.ReadPool(source.dataDirectory)
	if err != nil {
		return nil, err
	}
	source.mutex.Lock()
	defer source.mutex.Unlock()
	facts := make([]routing.AccountRoutingFact, 0, len(pool))
	for _, account := range pool {
		if fact, observed := source.cachedFacts[store.AccountName(account.Name)]; observed && fact.Credentials.AccountID == account.AccountID {
			facts = append(facts, fact)
		}
	}
	return facts, nil
}

func (source *poolAccountRoutingFactsSource) RequestAccountRoutingFactsRefresh() {
	select {
	case source.refreshRequested <- struct{}{}:
	default:
	}
}

// An explicit pre-admission rejection updates the cached catalogue immediately.
// An in-flight catalogue read cannot overwrite this newer observation.
func (source *poolAccountRoutingFactsSource) ExcludeUnsupportedAccountModel(accountName store.AccountName, modelSlug string) {
	source.mutex.Lock()
	fact := source.cachedFacts[accountName]
	fact.SupportedModelSlugs = slices.DeleteFunc(slices.Clone(fact.SupportedModelSlugs), func(slug string) bool { return slug == modelSlug })
	source.cachedFacts[accountName] = fact
	source.mutex.Unlock()
	source.RequestAccountRoutingFactsRefresh()
}

// RefreshAccountCredentialsAfterUnauthorized prepares the account for the next routed request.
func (source *poolAccountRoutingFactsSource) RefreshAccountCredentialsAfterUnauthorized(accountName store.AccountName, rejected accountapi.Credentials) error {
	pool, err := accountapi.ReadPool(source.dataDirectory)
	if err != nil {
		return err
	}
	for _, account := range pool {
		if store.AccountName(account.Name) != accountName {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		refreshed, refreshErr := source.credentialStoreFor(account).RefreshAfterUnauthorized(ctx, rejected)
		cancel()
		if refreshErr != nil {
			if accountapi.RequiresSignIn(refreshErr) {
				source.mutex.Lock()
				if fact, cached := source.cachedFacts[accountName]; cached && fact.Credentials == rejected {
					fact.AccountState = accountapi.AccountSignInRequired
					fact.AccountStateCause = refreshErr
					source.cachedFacts[accountName] = fact
				}
				source.mutex.Unlock()
			}
			return refreshErr
		}
		source.mutex.Lock()
		if fact, cached := source.cachedFacts[accountName]; cached {
			fact.Credentials = refreshed
			source.cachedFacts[accountName] = fact
		}
		source.mutex.Unlock()
		return nil
	}
	return fmt.Errorf("account %s is not in the pool", accountName)
}

func (source *poolAccountRoutingFactsSource) credentialStoreFor(account accountapi.PoolAccount) *accountapi.CredentialStore {
	name := store.AccountName(account.Name)
	source.mutex.Lock()
	defer source.mutex.Unlock()
	if existing, ok := source.credentialStores[name]; ok {
		return existing
	}
	created := accountapi.NewCredentialStore(filepath.Join(source.dataDirectory, account.Name), source.httpClient)
	source.credentialStores[name] = created
	return created
}

// fetchAccountRoutingFact reads current credentials, allowance and model availability.
// Failures are carried on the fact, and the refresh loop logs a change of state
// once: the provider, not a monitoring gap, decides limits.
func (source *poolAccountRoutingFactsSource) fetchAccountRoutingFact(parent context.Context, account accountapi.PoolAccount) routing.AccountRoutingFact {
	name := store.AccountName(account.Name)
	source.mutex.Lock()
	fact := source.cachedFacts[name]
	source.mutex.Unlock()
	if fact.Credentials.AccountID != account.AccountID {
		fact = routing.AccountRoutingFact{Credentials: accountapi.Credentials{AccountID: account.AccountID}}
	}
	fact.AccountName = name
	fact.AccountStateCause = nil
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	credentialStore := source.credentialStoreFor(account)
	credentials, err := credentialStore.Credentials(ctx)
	if err != nil {
		if accountapi.RequiresSignIn(err) || fact.AccountState == accountapi.AccountSignInRequired {
			fact.AccountState = accountapi.AccountSignInRequired
		} else {
			fact.AccountState = accountapi.AccountCredentialsUnavailable
		}
		fact.AccountStateCause = err
		return fact
	}
	fact.Credentials = credentials

	sample, err := source.fetchAllowanceSample(ctx, account, credentials)
	if errors.Is(err, errWhamUsageUnauthorized) {
		credentials, err = credentialStore.RefreshAfterUnauthorized(ctx, credentials)
		if err == nil {
			fact.Credentials = credentials
			sample, err = source.fetchAllowanceSample(ctx, account, credentials)
		}
	}
	if err != nil {
		if accountapi.RequiresSignIn(err) || fact.AccountState == accountapi.AccountSignInRequired {
			fact.AccountState = accountapi.AccountSignInRequired
		} else if fact.AllowanceSample.ObservedAt.IsZero() {
			fact.AccountState = accountapi.AccountUsageCheckFailed
		} else if fact.AllowanceSample.LimitReached || !fact.AllowanceSample.Allowed {
			fact.AccountState = accountapi.AccountLimited
		} else {
			fact.AccountState = accountapi.AccountReady
		}
		fact.AccountStateCause = err
	} else if sample.LimitReached || !sample.Allowed {
		fact.AccountState = accountapi.AccountLimited
	} else {
		fact.AccountState = accountapi.AccountReady
	}

	if err == nil {
		fact.AllowanceSample = sample
		fact.RefreshedAt = startedAt
	}
	source.mutex.Lock()
	if cached := source.cachedFacts[name]; cached.Credentials.AccountID == account.AccountID {
		fact.SupportedModelSlugs = cached.SupportedModelSlugs
	}
	source.mutex.Unlock()
	modelContext, cancelModels := context.WithTimeout(parent, 15*time.Second)
	defer cancelModels()
	models, modelErr := source.fetchSupportedModelSlugs(modelContext, fact.Credentials)
	fact.ModelListCause = modelErr
	if modelErr == nil {
		fact.SupportedModelSlugs = models
	}
	return fact
}

func (source *poolAccountRoutingFactsSource) fetchAllowanceSample(ctx context.Context, account accountapi.PoolAccount, credentials accountapi.Credentials) (accountapi.AllowanceSample, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.usageURL, nil)
	if err != nil {
		return accountapi.AllowanceSample{}, err
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	request.Header.Set("ChatGPT-Account-ID", credentials.AccountID)
	response, err := source.httpClient.Do(request)
	if err != nil {
		return accountapi.AllowanceSample{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		statusError := fmt.Errorf("wham/usage returned HTTP %d for %s", response.StatusCode, account.Name)
		if response.StatusCode == http.StatusUnauthorized {
			return accountapi.AllowanceSample{}, fmt.Errorf("%w: %v", errWhamUsageUnauthorized, statusError)
		}
		return accountapi.AllowanceSample{}, statusError
	}
	var usage accountapi.UsageResponse
	if err := json.NewDecoder(response.Body).Decode(&usage); err != nil {
		return accountapi.AllowanceSample{}, err
	}
	sample, err := usage.Sample(account.Name, credentials.AccountID, time.Now())
	if err != nil {
		return accountapi.AllowanceSample{}, err
	}
	for _, window := range []*accountapi.Window{usage.RateLimit.Primary, usage.RateLimit.Secondary} {
		if window == nil || *window.UsedPercent <= 0 || window.ResetAt == nil || *window.ResetAt <= 0 {
			continue
		}
		if err := source.storeDatabase.RecordAccountAllowanceWindowIdentity(store.AccountAllowanceWindowIdentity{
			ProviderAccountID: store.ProviderAccountID(sample.AccountID),
			DurationSeconds:   *window.DurationSeconds,
			ResetsAt:          time.Unix(*window.ResetAt, 0).UTC(),
			ObservedAt:        sample.ObservedAt,
			UsedPercent:       *window.UsedPercent,
		}); err != nil {
			log.Printf("%s: could not record its allowance window: %v", account.Name, err)
		}
	}
	return sample, nil
}

func (source *poolAccountRoutingFactsSource) fetchSupportedModelSlugs(ctx context.Context, credentials accountapi.Credentials) ([]string, error) {
	clientVersion, err := codexClientVersion(ctx, source.codexExecutable)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.modelsURL, nil)
	if err != nil {
		return nil, err
	}
	query := request.URL.Query()
	query.Set("client_version", clientVersion)
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	request.Header.Set("ChatGPT-Account-ID", credentials.AccountID)
	response, err := source.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models returned HTTP %d", response.StatusCode)
	}
	var catalogue struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.NewDecoder(response.Body).Decode(&catalogue); err != nil {
		return nil, err
	}
	if catalogue.Models == nil {
		return nil, errors.New("models response omitted models")
	}
	slugs := make([]string, 0, len(catalogue.Models))
	for _, model := range catalogue.Models {
		if model.Slug == "" {
			return nil, errors.New("models response contains empty slug")
		}
		slugs = append(slugs, model.Slug)
	}
	return slugs, nil
}
