package accounts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMalformedAccountDoesNotDisableHealthyPool(t *testing.T) {
	root := t.TempDir()
	healthy := writeAccountFixture(t, filepath.Join(root, "account-1"), "user-one", "one@example.test", "synthetic-access")
	os.Mkdir(filepath.Join(root, "account-2"), 0700)
	os.WriteFile(filepath.Join(root, "account-2", "auth.json"), []byte("{"), 0600)
	pool, err := ReadPool(root)
	if err != nil || len(pool) != 2 {
		t.Fatalf("healthy pool blocked by one malformed account: %v, count %d", err, len(pool))
	}
	if pool[0].AccountID != healthy.AccountID || pool[1].Name != "account-2" || pool[1].AccountID != "" {
		t.Fatal("lost healthy identity or admitted invalid identity")
	}
	// A broken entry remains removable by its storage name, without parsing it.
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "trash"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if _, _, err := RemoveAccount(root, "account-2"); err != nil {
		t.Fatal(err)
	}
	pool, err = ReadPool(root)
	if err != nil || len(pool) != 1 {
		t.Fatalf("removal: %v", err)
	}
}

func TestRefreshInvalidGrantRequiresSignIn(t *testing.T) {
	for _, status := range []int{400, 401, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			root := t.TempDir()
			writeTestAuth(t, root, jwt(t, time.Now().Add(-time.Minute), "account"), "synthetic-refresh", "account")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				w.Write([]byte(`{"error":"invalid_grant","error_description":"private-token"}`))
			}))
			defer server.Close()
			login := newSavedLogin(root, server.Client())
			login.token = server.URL
			_, err := login.Credentials(t.Context())
			if err == nil || requiresSignIn(err) != (status == 400 || status == 401) || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("wrong classification or exposed response: %v", err)
			}
		})
	}
}

func TestCredentialLockHonorsContextDeadline(t *testing.T) {
	root := t.TempDir()
	writeTestAuth(t, root, jwt(t, time.Now().Add(time.Hour), "account"), "synthetic-refresh", "account")
	locked, release, holderDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(holderDone)
		withAuthLock(root, func() error { close(locked); <-release; return nil })
	}()
	<-locked
	defer func() { close(release); <-holderDone }()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	login := newSavedLogin(root, &http.Client{})
	done := make(chan error, 1)
	go func() { _, err := login.Credentials(ctx); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("credential read ignored its deadline while waiting for another writer")
	}
}
