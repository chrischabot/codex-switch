package clientcapability

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testCapability = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"

func TestAuthenticatedRequestStripsCapabilityAndPreservesRequest(t *testing.T) {
	var receivedPath, receivedRawPath, receivedQuery, receivedRequestURI, receivedBody string
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedPath = request.URL.Path
		receivedRawPath = request.URL.RawPath
		receivedQuery = request.URL.RawQuery
		receivedRequestURI = request.RequestURI
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		receivedBody = string(body)
		response.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"http://codex-switch/client/"+testCapability+"/responses/a%20b?stream=true&value=a%2Fb",
		strings.NewReader("request body"),
	)
	response := httptest.NewRecorder()
	RequirePathPrefix(testCapability, next).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if receivedPath != "/responses/a b" || receivedRawPath != "" ||
		receivedQuery != "stream=true&value=a%2Fb" ||
		receivedRequestURI != "/responses/a%20b?stream=true&value=a%2Fb" ||
		receivedBody != "request body" {
		t.Fatalf(
			"forwarded request = path %q rawPath %q query %q requestURI %q body %q",
			receivedPath, receivedRawPath, receivedQuery, receivedRequestURI, receivedBody,
		)
	}
}

func TestAuthenticatedBareCapabilityMapsToRoot(t *testing.T) {
	var receivedPath, receivedRequestURI string
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedPath = request.URL.Path
		receivedRequestURI = request.RequestURI
		response.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"http://codex-switch/client/"+testCapability+"?model=codex",
		nil,
	)
	response := httptest.NewRecorder()

	RequirePathPrefix(testCapability, next).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || receivedPath != "/" || receivedRequestURI != "/?model=codex" {
		t.Fatalf("root request = status %d path %q requestURI %q", response.Code, receivedPath, receivedRequestURI)
	}
}

func TestRejectedPathsDoNotReachOrReadNextHandler(t *testing.T) {
	tests := []string{
		"/",
		"/client",
		"/client/",
		"/client/" + testCapability[:42],
		"/client/" + strings.Repeat("x", encodedCapabilityLength),
		"/client/" + testCapability + "x",
		"/client/" + testCapability + "x/responses",
		"/client/" + testCapability + "%2Fresponses",
	}
	for _, requestPath := range tests {
		t.Run(requestPath, func(t *testing.T) {
			body := &observedBody{}
			request := httptest.NewRequest(http.MethodPost, "http://codex-switch"+requestPath, nil)
			request.Body = body
			response := httptest.NewRecorder()
			nextCalled := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })

			RequirePathPrefix(testCapability, next).ServeHTTP(response, request)

			if response.Code != http.StatusNotFound || nextCalled || body.read {
				t.Fatalf("rejection = status %d next %t body-read %t", response.Code, nextCalled, body.read)
			}
			if strings.Contains(response.Body.String(), testCapability) {
				t.Fatal("not-found response revealed the capability")
			}
		})
	}
}

func TestAuthenticatedSSEPassesThroughStreamingResponse(t *testing.T) {
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/events" {
			t.Fatalf("SSE path = %q", request.URL.Path)
		}
		flusher, ok := response.(http.Flusher)
		if !ok {
			t.Fatal("authenticated response writer lost http.Flusher")
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(response, "data: first\n\n")
		flusher.Flush()
		_, _ = io.WriteString(response, "data: second\n\n")
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"http://codex-switch/client/"+testCapability+"/events",
		nil,
	)
	response := httptest.NewRecorder()

	RequirePathPrefix(testCapability, next).ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" ||
		response.Body.String() != "data: first\n\ndata: second\n\n" || !response.Flushed {
		t.Fatalf("SSE response = status %d headers %#v body %q flushed %t", response.Code, response.Header(), response.Body.String(), response.Flushed)
	}
}

func TestAuthenticatedWebSocketUpgradeHeadersPassThrough(t *testing.T) {
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" || request.Header.Get("Upgrade") != "websocket" ||
			request.Header.Get("Connection") != "Upgrade" || request.Header.Get("Sec-WebSocket-Key") != "test-key" {
			t.Fatalf("WebSocket request changed: path %q headers %#v", request.URL.Path, request.Header)
		}
		response.Header().Set("Upgrade", "websocket")
		response.Header().Set("Connection", "Upgrade")
		response.WriteHeader(http.StatusSwitchingProtocols)
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"http://codex-switch/client/"+testCapability+"/responses",
		nil,
	)
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Sec-WebSocket-Key", "test-key")
	response := httptest.NewRecorder()

	RequirePathPrefix(testCapability, next).ServeHTTP(response, request)

	if response.Code != http.StatusSwitchingProtocols || response.Header().Get("Upgrade") != "websocket" ||
		response.Header().Get("Connection") != "Upgrade" {
		t.Fatalf("WebSocket response = status %d headers %#v", response.Code, response.Header())
	}
}

type observedBody struct {
	read bool
}

func (body *observedBody) Read([]byte) (int, error) {
	body.read = true
	return 0, io.EOF
}

func (*observedBody) Close() error { return nil }
