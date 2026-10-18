package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/joshp123/codex-switch/internal/store"
)

// observeRejectedResponse logs every request OpenAI answers with HTTP 400 or
// above, with the reason OpenAI gave. A model request rejected before OpenAI
// started work because its account is out of allowance, or does not offer the
// model, moves its task and becomes a retryable error for Codex; the proxy never
// resends the request itself. Every other rejection reaches the client unchanged.
func (handler *TrafficRelayHandler) observeRejectedResponse(routedRequest *routedRelayRequest, upstreamResponse *http.Response) error {
	subject := describeModelRequest(routedRequest.taskID, routedRequest.client, routedRequest.routeDecision.DisplayName)
	if routedRequest.workKind != store.ModelTurnWorkKind {
		// Read only the start of the body for the reason, then hand the client
		// every byte unchanged.
		head, _ := io.ReadAll(io.LimitReader(upstreamResponse.Body, 4096))
		upstreamResponse.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(head), upstreamResponse.Body), upstreamResponse.Body}
		log.Printf("%s: OpenAI answered %s %s with HTTP %d%s", subject, upstreamResponse.Request.Method, upstreamResponse.Request.URL.Path, upstreamResponse.StatusCode, describeRejection(upstreamResponse.Header, head))
		return nil
	}
	if upstreamResponse.StatusCode == http.StatusUnauthorized {
		handler.taskRouter.ReportUnauthorizedTurn(routedRequest.routeDecision.AccountName, routedRequest.routeDecision.Credentials)
	}
	responseObserver := routedRequest.responseObserver
	responseObserver.settleModelRequest("", store.ModelRequestFailed)
	responseObserver.admittedRequests = 0

	body, err := io.ReadAll(upstreamResponse.Body)
	upstreamResponse.Body.Close()
	if err != nil {
		log.Printf("%s: OpenAI rejected %s with HTTP %d and its reason could not be read: %v", subject, routedRequest.modelSlug, upstreamResponse.StatusCode, err)
		return fmt.Errorf("read rejected upstream response: %w", err)
	}
	upstreamResponse.Body = io.NopCloser(bytes.NewReader(body))
	var rejected httpJSONResponseWire
	_ = json.Unmarshal(body, &rejected)
	if limitOutcome, limited := responseObserver.reportProviderUsageLimit(rejected.Error); limited {
		log.Printf("%s: %s; Codex retries", subject, limitOutcome)
		return errUpstreamRejectedBeforeAdmission
	}
	if responseObserver.reportUnsupportedModel(rejected.Error) || responseObserver.reportUnsupportedModelDetail(rejected.Detail) {
		log.Printf("%s: OpenAI says this account does not offer %s; Codex retries, and the retry goes to an account that offers it or waits for one", subject, routedRequest.modelSlug)
		return errUpstreamRejectedBeforeAdmission
	}
	log.Printf("%s: OpenAI rejected %s with HTTP %d%s", subject, routedRequest.modelSlug, upstreamResponse.StatusCode, describeRejection(upstreamResponse.Header, body))
	return nil
}

// describeRejection keeps only known classifications. Provider bodies and
// headers can echo credentials or prompt text, including inside error types.
func describeRejection(header http.Header, body []byte) string {
	if header.Get("Cf-Mitigated") != "" {
		return ": Cloudflare stopped the request before OpenAI"
	}
	var rejected httpJSONResponseWire
	if json.Unmarshal(body, &rejected) == nil {
		return describeProviderReason(rejected)
	}
	return ": non-JSON error response"
}

func describeProviderReason(rejected httpJSONResponseWire) string {
	if rejected.Error == nil {
		return ": provider error; details omitted"
	}
	switch rejected.Error.Type {
	case "server_error", "invalid_request_error", "authentication_error", "permission_error", "rate_limit_error", "usage_limit_reached", "insufficient_quota", "not_found_error", "overloaded_error", "api_error":
		return ": " + rejected.Error.Type
	case "incomplete":
		switch rejected.Error.Message {
		case "max_output_tokens", "content_filter":
			return ": incomplete " + rejected.Error.Message
		}
		return ": incomplete"
	default:
		return ": provider error; details omitted"
	}
}
