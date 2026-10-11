package main

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/menupb"
	"github.com/josh/codex-switch/internal/routing"
	"github.com/josh/codex-switch/internal/store"
)

func TestSnapshotKeepsAccountThatRequiresSignIn(t *testing.T) {
	data := t.TempDir()
	writeAccountFixture(t, filepath.Join(data, "account-1"), "signed-out", "account@example.test", "")
	database, err := store.OpenStoreDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.CloseStoreDatabase()
	origin, _ := url.Parse("http://127.0.0.1:1")
	monitor := accountapi.NewMonitor(data, origin, "", http.DefaultClient, database)
	router := routing.CreateTaskRouter(database, monitor)
	if _, err := readServingMenuSnapshot(data, router, "astra"); err == nil {
		t.Fatal("published an empty pool before observing the installed account")
	}
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() { defer close(stopped); monitor.Run(ctx) }()
	defer func() { cancel(); <-stopped }()
	monitor.WaitForFirstRead()
	snapshot, err := readServingMenuSnapshot(data, router, "astra")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Accounts) != 1 || snapshot.Accounts[0].Usable || snapshot.Accounts[0].State != menupb.AccountState_ACCOUNT_STATE_SIGN_IN_REQUIRED {
		t.Fatalf("signed-out account disappeared or became usable: %v", snapshot.Accounts)
	}
}
