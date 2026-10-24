package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joshp123/codex-switch/internal/clientcapability"
)

func TestProfilesRequireClientCapability(t *testing.T) {
	const capability = "private-profiling-capability"
	privatePath := clientcapability.PathPrefix(capability) + "/debug/pprof/goroutine?debug=1"
	handler := profileOrRelay(capability, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "relay", http.StatusForbidden) }))
	for _, path := range []string{privatePath, "/debug/pprof/goroutine?debug=1", strings.Replace(privatePath, capability, "wrong", 1)} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if path == privatePath {
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "goroutine profile:") {
				t.Fatalf("private profile: HTTP %d", response.Code)
			}
		} else if response.Code != http.StatusForbidden {
			t.Fatalf("public profile returned HTTP %d", response.Code)
		}
	}
}
