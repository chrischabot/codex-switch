package relay

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/joshp123/codex-switch/internal/store"
)

type ClaudeAccountObserver interface {
	ObserveClaudeResponse(store.AccountName, string, http.Header, int)
}

func prepareClaudeHeaders(headers http.Header) {
	headers.Del("X-Api-Key")
	headers.Del(chatGPTAccountIDHeader)
	if headers.Get("Anthropic-Version") == "" {
		headers.Set("Anthropic-Version", "2023-06-01")
	}
	for _, beta := range []string{"oauth-2025-04-20", "claude-code-20250219"} {
		found := false
		for _, existing := range strings.Split(headers.Get("Anthropic-Beta"), ",") {
			if strings.TrimSpace(existing) == beta {
				found = true
			}
		}
		if !found {
			value := headers.Get("Anthropic-Beta")
			if value != "" {
				value += ","
			}
			headers.Set("Anthropic-Beta", value+beta)
		}
	}
}

func (handler *TrafficRelayHandler) observeClaudeRejection(request *routedRelayRequest, response *http.Response) error {
	if request.workKind != store.ModelTurnWorkKind {
		return nil
	}
	request.responseObserver.settleModelRequest("", store.ModelRequestFailed)
	request.responseObserver.admittedRequests = 0
	switch response.StatusCode {
	case http.StatusTooManyRequests:
		// The next request chooses from the whole pool. No rejected request is replayed.
		response.Header.Set("Retry-After", "1")
		response.Header.Set("X-Should-Retry", "true")
		log.Printf("%s: Anthropic limited %s; the client's next request uses available capacity", describeModelRequest(request.taskID, request.client, request.routeDecision.AccountName), request.modelSlug)
	case http.StatusUnauthorized:
		handler.taskRouter.ReportUnauthorizedTurn(request.routeDecision.AccountName, request.routeDecision.Credentials)
		response.StatusCode = http.StatusServiceUnavailable
		response.Status = "503 Service Unavailable"
		response.Header.Set("Retry-After", "1")
		response.Header.Set("X-Should-Retry", "true")
	default:
		log.Printf("%s: Anthropic answered %s with HTTP %d", describeModelRequest(request.taskID, request.client, request.routeDecision.AccountName), request.modelSlug, response.StatusCode)
	}
	return nil
}

func (observer *terminalResponseObserver) observeClaudeUsage(payload []byte) {
	complete, err := observer.claudeUsage.Observe(payload)
	if err != nil {
		log.Printf("%s: read Claude usage: %v", observer.describe(), err)
		return
	}
	if !complete {
		return
	}
	record, err := observer.claudeUsage.Record(observer.taskID, observer.accountName, time.Now().UTC())
	if err != nil {
		log.Printf("%s: price Claude usage: %v", observer.describe(), err)
		return
	}
	observer.settleModelRequest("", store.ModelRequestSuccessful)
	observer.admittedRequests = 0
	if err := observer.completionLedger.RecordCompletedResponse(record); err != nil {
		log.Printf("%s: record Claude usage: %v", observer.describe(), err)
	}
}
