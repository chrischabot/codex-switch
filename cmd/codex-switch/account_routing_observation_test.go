package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/menupb"
	"github.com/josh/codex-switch/internal/routing"
	"github.com/josh/codex-switch/internal/store"
)

func TestSnapshotKeepsAccountThatRequiresSignIn(t *testing.T) {
	data := t.TempDir()
	account := writeAccountFixture(t, filepath.Join(data, "account-1"), "signed-out", "account@example.test", "")
	database := routingFactsTestDatabase(t)
	origin, _ := url.Parse("http://127.0.0.1:1")
	source := newPoolAccountRoutingFactsSource(data, origin, http.DefaultClient, database)
	router := routing.CreateTaskRouter(database, source)
	if _, err := readServingMenuSnapshot(source, router, "astra"); err == nil {
		t.Fatal("published an empty pool before observing the installed account")
	}
	source.cachedFacts[store.AccountName(account.Name)] = source.fetchAccountRoutingFact(t.Context(), account)
	snapshot, err := readServingMenuSnapshot(source, router, "astra")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Accounts) != 1 || snapshot.Accounts[0].Usable || snapshot.Accounts[0].State != menupb.AccountState_ACCOUNT_STATE_SIGN_IN_REQUIRED {
		t.Fatalf("signed-out account disappeared or became usable: %v", snapshot.Accounts)
	}
}

func TestAllowanceMonitoringFailurePreservesObservedExhaustion(t *testing.T) {
	data := t.TempDir()
	writeAccountFixture(t, filepath.Join(data, "account-1"), "observed-user", "observed@example.test", processAccessToken("observed-user"))
	status := http.StatusOK
	limited := true
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backend-api/codex/models" {
			_, _ = w.Write([]byte(`{"models":[{"slug":"astra"}]}`))
			return
		}
		w.WriteHeader(status)
		if limited {
			_, _ = w.Write([]byte(`{"plan_type":"pro","rate_limit":{"allowed":false,"limit_reached":true}}`))
		} else {
			_, _ = w.Write([]byte(`{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false}}`))
		}
	}))
	defer upstream.Close()
	origin, _ := url.Parse(upstream.URL)
	source := newPoolAccountRoutingFactsSource(data, origin, upstream.Client(), routingFactsTestDatabase(t))
	pool, err := accountapi.ReadPool(data)
	if err != nil {
		t.Fatal(err)
	}
	first := source.fetchAccountRoutingFact(t.Context(), pool[0])
	source.cachedFacts[store.AccountName("account-1")] = first
	status = http.StatusServiceUnavailable
	failed := source.fetchAccountRoutingFact(t.Context(), pool[0])
	if failed.AccountState != accountapi.AccountLimited || !failed.RefreshedAt.Equal(first.RefreshedAt) || !failed.AllowanceSample.LimitReached {
		t.Fatalf("monitor failure erased exhaustion: state=%s observation=%v", failed.AccountState, failed.RefreshedAt)
	}
	source.cachedFacts[store.AccountName("account-1")] = failed
	status, limited = http.StatusOK, false
	restored := source.fetchAccountRoutingFact(t.Context(), pool[0])
	if restored.AccountState != accountapi.AccountReady || !restored.RefreshedAt.After(first.RefreshedAt) {
		t.Fatalf("new observation did not restore capacity: %s", restored.AccountState)
	}
}

func TestAccountRefreshDoesNotBlockOtherAccountsOrCachedReads(t *testing.T) {
	data := t.TempDir()
	for _, name := range []string{"account-1", "account-2"} {
		writeAccountFixture(t, filepath.Join(data, name), name, name+"@example.test", processAccessToken(name))
	}
	delayed := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("ChatGPT-Account-ID") == "subscription-account-1" {
			select {
			case delayed <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		if r.URL.Path == "/backend-api/codex/models" {
			_, _ = w.Write([]byte(`{"models":[{"slug":"astra"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false}}`))
	}))
	defer upstream.Close()
	origin, _ := url.Parse(upstream.URL)
	source := newPoolAccountRoutingFactsSource(data, origin, upstream.Client(), routingFactsTestDatabase(t))
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() { defer close(stopped); source.Run(ctx) }()
	defer func() { cancel(); <-stopped }()
	select {
	case <-delayed:
	case <-time.After(time.Second):
		t.Fatal("delayed account was not read")
	}
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		facts, err := source.ReadAccountRoutingFacts()
		if err != nil {
			t.Fatal(err)
		}
		for _, fact := range facts {
			if fact.AccountName == "account-2" && fact.AccountState == accountapi.AccountReady {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatal("healthy account waited for delayed account")
		case <-ticker.C:
		}
	}
}

func routingFactsTestDatabase(t *testing.T) *store.StoreDatabase {
	t.Helper()
	database, err := store.OpenStoreDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.CloseStoreDatabase() })
	return database
}

func TestAllowanceIdentityRequiresPositiveUsageAndExplicitReset(t *testing.T) {
	for _, test := range []struct {
		name        string
		usedPercent int
		resetFields string
		wantOwner   bool
	}{
		{"explicit used window", 54, `"reset_at":1789163887`, true},
		{"unused window", 0, `"reset_at":1789163887`, false},
		{"countdown only", 54, `"reset_after_seconds":300`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":%d,"limit_window_seconds":604800,%s}}}`, test.usedPercent, test.resetFields)
			}))
			defer server.Close()
			database := routingFactsTestDatabase(t)
			origin, _ := url.Parse(server.URL)
			source := newPoolAccountRoutingFactsSource(t.TempDir(), origin, server.Client(), database)
			sample, err := source.fetchAllowanceSample(t.Context(), accountapi.PoolAccount{Name: "account-1"}, accountapi.Credentials{AccountID: "subscription-1"})
			if err != nil {
				t.Fatal(err)
			}
			window := sample.Windows[0]
			owner, found, err := database.FindAccountForAllowanceWindow(window.DurationSeconds, window.ResetsAt, window.ResetsAt.Add(-time.Second))
			if err != nil || found != test.wantOwner || (found && owner != "subscription-1") {
				t.Fatalf("allowance owner = %q, found = %v, error = %v", owner, found, err)
			}
		})
	}
}

type accountRevocationTransport func(*http.Request) (*http.Response, error)

func (f accountRevocationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRevokedLoginIsUnavailableUntilSignInRepairsIt(t *testing.T) {
	for _, rejectionSource := range []string{"allowance observation", "model request", "model request during allowance observation"} {
		t.Run(rejectionSource, func(t *testing.T) {
			data := t.TempDir()
			account := writeAccountFixture(t, filepath.Join(data, "account-1"), "revoked-user", "revoked@example.test", processAccessToken("revoked-user"))
			revoked := false
			allowanceUnavailable := false
			holdAllowance := false
			allowanceStarted := make(chan struct{})
			releaseAllowance := make(chan struct{})
			allowanceReleased := make(chan struct{})
			repairedAccessToken := processAccessToken("repaired-user")
			client := &http.Client{Transport: accountRevocationTransport(func(r *http.Request) (*http.Response, error) {
				status, body := 200, `{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false}}`
				if holdAllowance && r.URL.Path == "/backend-api/wham/usage" {
					close(allowanceStarted)
					select {
					case <-releaseAllowance:
						close(allowanceReleased)
						return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				}
				if r.URL.Path == "/backend-api/codex/models" {
					body = `{"models":[{"slug":"astra"}]}`
				}
				if revoked && r.Header.Get("Authorization") != "Bearer "+repairedAccessToken {
					status = 401
					body = `{"error":{"code":"refresh_token_reused"}}`
				}
				if allowanceUnavailable && r.URL.Path == "/backend-api/wham/usage" {
					status = http.StatusServiceUnavailable
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})}
			origin, _ := url.Parse("http://isolated-lifecycle.invalid")
			database := routingFactsTestDatabase(t)
			source := newPoolAccountRoutingFactsSource(data, origin, client, database)
			source.cachedFacts["account-1"] = source.fetchAccountRoutingFact(t.Context(), account)
			router := routing.CreateTaskRouter(database, source)
			before, err := readServingMenuSnapshot(source, router, "astra")
			if err != nil {
				t.Fatal(err)
			}
			if !before.Accounts[0].Usable {
				t.Fatal("operating account was not usable before revocation")
			}
			rejectedCredentials := source.cachedFacts["account-1"].Credentials
			revoked = true
			if rejectionSource == "model request during allowance observation" {
				holdAllowance = true
				ctx, cancel := context.WithCancel(t.Context())
				stopped := make(chan struct{})
				go func() { defer close(stopped); source.Run(ctx) }()
				t.Cleanup(func() { cancel(); <-stopped })
				select {
				case <-allowanceStarted:
				case <-time.After(time.Second):
					t.Fatal("background allowance observation did not start")
				}
				router.ReportUnauthorizedTurn("account-1", rejectedCredentials)
				close(releaseAllowance)
				<-allowanceReleased
				cancel()
				<-stopped
				holdAllowance = false
			} else if rejectionSource == "model request" {
				router.ReportUnauthorizedTurn("account-1", source.cachedFacts["account-1"].Credentials)
			} else {
				source.cachedFacts["account-1"] = source.fetchAccountRoutingFact(t.Context(), account)
			}
			after, err := readServingMenuSnapshot(source, router, "astra")
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			writeCurrentStatus(&out, after)
			t.Log(out.String())
			if after.Accounts[0].Usable || after.Accounts[0].State != menupb.AccountState_ACCOUNT_STATE_SIGN_IN_REQUIRED {
				t.Fatalf("revoked login status is %s, usable=%t", after.Accounts[0].State, after.Accounts[0].Usable)
			}
			allowanceUnavailable = true
			source.cachedFacts["account-1"] = source.fetchAccountRoutingFact(t.Context(), account)
			afterOutage, err := readServingMenuSnapshot(source, router, "astra")
			if err != nil {
				t.Fatal(err)
			}
			if afterOutage.Accounts[0].Usable || afterOutage.Accounts[0].State != menupb.AccountState_ACCOUNT_STATE_SIGN_IN_REQUIRED {
				t.Fatal("allowance outage revived the rejected login")
			}
			staged := filepath.Join(t.TempDir(), "login")
			repaired := writeAccountFixture(t, staged, "revoked-user", "revoked@example.test", repairedAccessToken)
			if _, err = publishLogin(data, staged, account.Name, repaired); err != nil {
				t.Fatal(err)
			}
			allowanceUnavailable = false
			if rejectionSource == "model request during allowance observation" {
				source.lastAttempt = map[store.AccountName]time.Time{}
				ctx, cancel := context.WithCancel(t.Context())
				stopped := make(chan struct{})
				go func() { defer close(stopped); source.Run(ctx) }()
				t.Cleanup(func() { cancel(); <-stopped })
				deadline := time.After(time.Second)
				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				for {
					facts, err := source.ReadAccountRoutingFacts()
					if err != nil {
						t.Fatal(err)
					}
					if facts[0].AccountState == accountapi.AccountReady {
						break
					}
					select {
					case <-deadline:
						t.Fatal("background observation did not restore repaired login")
					case <-ticker.C:
					}
				}
				cancel()
				<-stopped
			} else {
				source.cachedFacts["account-1"] = source.fetchAccountRoutingFact(t.Context(), account)
			}
			restored, err := readServingMenuSnapshot(source, router, "astra")
			if err != nil {
				t.Fatal(err)
			}
			if !restored.Accounts[0].Usable || restored.Accounts[0].State != menupb.AccountState_ACCOUNT_STATE_READY {
				t.Fatalf("repaired login remains unavailable: %v", restored.Accounts[0])
			}
		})
	}
}
