package relay

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/josh/codex-switch/internal/store"
	"github.com/josh/codex-switch/internal/taskidentity"
)

func taskIdentityForRequest(request *http.Request) store.CodexTaskID {
	if taskID := strings.TrimSpace(request.Header.Get(codexTaskIDHeader)); taskID != "" {
		return store.CodexTaskID(taskID)
	}
	if request.Header.Get("Originator") != "pi" || workKindForRequest(request) != store.ModelTurnWorkKind {
		return ""
	}
	if request.Method != http.MethodPost && !(request.Method == http.MethodGet && request.URL.Path == codexResponsesPath && strings.EqualFold(request.Header.Get("Upgrade"), "websocket")) {
		return ""
	}
	sessionID := strings.TrimSpace(request.Header.Get("Session-ID"))
	if sessionID == "" {
		return ""
	}
	return store.CodexTaskID(taskidentity.PiSession(sessionID))
}

// clientNameForRequest names the sending client in log lines, as the client
// names itself: the first User-Agent product token, for example
// "codex_exec/0.162.0", or its Originator header.
func clientNameForRequest(request *http.Request) string {
	if fields := strings.Fields(request.Header.Get("User-Agent")); len(fields) > 0 {
		return fields[0]
	}
	if originator := strings.TrimSpace(request.Header.Get("Originator")); originator != "" {
		return originator
	}
	return "an unnamed client"
}

// describeModelRequest names a routed model request in log lines: its task, the
// client that sent it and the account serving it.
func describeModelRequest(codexTaskID store.CodexTaskID, client string, accountName store.AccountName) string {
	work := "task " + string(codexTaskID)
	if codexTaskID == "" {
		work = "request without a task ID"
	}
	if accountName == "" {
		return fmt.Sprintf("%s from %s", work, client)
	}
	return fmt.Sprintf("%s from %s on %s", work, client, accountName)
}
