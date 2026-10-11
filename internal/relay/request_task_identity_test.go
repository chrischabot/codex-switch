package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPiSessionAffinityIsNamespacedAndCodexThreadTakesPrecedence(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, codexResponsesPath, nil)
	request.Header.Set("Originator", "pi")
	request.Header.Set("Session-ID", "01a00000-0000-7000-8000-000000000003")
	identity := taskIdentityForRequest(request)
	if identity != "8a34a342-82b2-8f9c-94e3-20724e1f5144" {
		t.Fatalf("Pi session identity changed: %q", identity)
	}
	request.Method = http.MethodGet
	request.Header.Set("Upgrade", "websocket")
	if taskIdentityForRequest(request) != identity {
		t.Fatal("WebSocket and HTTP must share the Pi session pin")
	}
	request.Header.Set("Thread-ID", "codex-thread")
	if taskIdentityForRequest(request) != "codex-thread" {
		t.Fatal("explicit Codex thread did not take precedence")
	}
	request.Header.Del("Thread-ID")
	request.Header.Del("Originator")
	if taskIdentityForRequest(request) != "" {
		t.Fatal("generic Session-ID must not create a task pin")
	}
	request.Header.Set("Originator", "pi")
	request.URL.Path = "/backend-api/codex/models"
	if taskIdentityForRequest(request) != "" {
		t.Fatal("metadata must not create a Pi task pin")
	}
}
