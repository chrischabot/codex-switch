package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	accountapi "github.com/josh/codex-switch/internal/accounts"
)

func TestAccountModelCatalogueContract(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	writeCodexModelCache(t, home, "0.153.4")
	executable := filepath.Join(t.TempDir(), "codex")
	writeCodexExecutableVersion(t, executable, "0.159.2")
	for _, test := range []struct {
		name, body string
		status     int
		want       []string
		wantError  bool
	}{
		{"slugs", `{"models":[{"slug":"gpt-5.6-luna","display_name":"Luna"}]}`, 200, []string{"gpt-5.6-luna"}, false},
		{"empty", `{"models":[]}`, 200, []string{}, false},
		{"missing", `{}`, 200, nil, true},
		{"invalid slug", `{"models":[{}]}`, 200, nil, true},
		{"unavailable", `{}`, 503, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/backend-api/codex/models" || r.URL.Query().Get("client_version") != "0.159.2" {
					t.Errorf("unexpected catalogue URL: %s", r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("ChatGPT-Account-ID") != "test-account" {
					t.Error("missing account authentication")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			origin, _ := url.Parse(server.URL)
			source := newPoolAccountRoutingFactsSource(t.TempDir(), origin, server.Client(), routingFactsTestDatabase(t))
			source.codexExecutable = executable
			got, err := source.fetchSupportedModelSlugs(context.Background(), accountapi.Credentials{AccessToken: "test-token", AccountID: "test-account"})
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v", err)
			}
			if !slices.Equal(got, test.want) || (got == nil) != (test.want == nil) {
				t.Fatalf("slugs = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestAccountModelCatalogueTracksSelectedClientUpgrade(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "codex")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("client_version") {
		case "0.153.1":
			_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-5.6-sol"}]}`))
		case "0.159.2":
			_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-5.6-sol"},{"slug":"gpt-6.1-sol"}]}`))
		default:
			http.Error(w, "client version required", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	origin, _ := url.Parse(server.URL)
	source := newPoolAccountRoutingFactsSource(t.TempDir(), origin, server.Client(), routingFactsTestDatabase(t))
	source.codexExecutable = executable
	for _, version := range []string{"0.153.1", "0.159.2"} {
		writeCodexExecutableVersion(t, executable, version)
		slugs, err := source.fetchSupportedModelSlugs(context.Background(), accountapi.Credentials{})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := slices.Contains(slugs, "gpt-6.1-sol"), version == "0.159.2"; got != want {
			t.Fatalf("Codex %s: gpt-6.1-sol availability = %t, want %t", version, got, want)
		}
	}
}

func writeCodexModelCache(t *testing.T, home, version string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(`{"client_version":"`+version+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}
