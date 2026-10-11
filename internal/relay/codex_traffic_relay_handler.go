package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/josh/codex-switch/internal/clientcapability"
	"github.com/josh/codex-switch/internal/routing"
	"github.com/josh/codex-switch/internal/store"
	"github.com/klauspost/compress/zstd"
)

const (
	codexResponsesPath        = "/backend-api/codex/responses"
	codexCompactResponsesPath = "/backend-api/codex/responses/compact"
	codexTaskIDHeader         = "Thread-ID"
	chatGPTAccountIDHeader    = "ChatGPT-Account-ID"
)

var errUpstreamRejectedBeforeAdmission = errors.New("upstream account rejected work before admission")

type relayRequestContextKey struct{}
type requestTrailerSourceContextKey struct{}

type routedRelayRequest struct {
	codexTaskID             store.CodexTaskID
	client                  string
	workKind                store.WorkKind
	modelSlug               string
	responseStreamRequested bool
	routeDecision           routing.RouteDecision
	requestTrailerSource    http.Header
	responseObserver        *terminalResponseObserver
}

// RelayHandlerOptions keeps listener and signal ownership outside the transport package.
type RelayHandlerOptions struct {
	UpstreamOrigin   *url.URL
	ClientCapability string
	TaskRouter       TaskRouter
	CompletionLedger CompletionLedger
	RequestCounters  ModelRequestCounters
	HTTPTransport    http.RoundTripper
}

// CodexTrafficRelayHandler stays alive with admitted streams so deployment does not cut off model work.
type CodexTrafficRelayHandler struct {
	authenticatedHandler http.Handler
	taskRouter           TaskRouter
	completionLedger     CompletionLedger
	requestCounters      ModelRequestCounters
	upstreamOrigin       *url.URL
	upstreamTransport    http.RoundTripper
	reverseProxy         *httputil.ReverseProxy

	drainMutex         sync.Mutex
	activeRequestCount int
	drainStarted       bool
	drainComplete      chan struct{}
	openWebSockets     map[*terminalResponseObserver]struct{}
	closingWebSockets  bool
}

// CreateCodexTrafficRelayHandler leaves listener ownership and signal handling with the command layer.
func CreateCodexTrafficRelayHandler(options RelayHandlerOptions) (*CodexTrafficRelayHandler, error) {
	if options.UpstreamOrigin == nil || options.UpstreamOrigin.Scheme == "" || options.UpstreamOrigin.Host == "" {
		return nil, errors.New("relay upstream origin is required")
	}
	if options.UpstreamOrigin.Scheme != "http" && options.UpstreamOrigin.Scheme != "https" {
		return nil, errors.New("relay upstream origin must use http or https")
	}
	if options.UpstreamOrigin.User != nil || options.UpstreamOrigin.Path != "" ||
		options.UpstreamOrigin.RawQuery != "" || options.UpstreamOrigin.Fragment != "" {
		return nil, errors.New("relay upstream origin must not include credentials, path, query, or fragment")
	}
	if options.TaskRouter == nil {
		return nil, errors.New("relay task router is required")
	}
	if options.CompletionLedger == nil {
		return nil, errors.New("relay completion ledger is required")
	}

	upstreamTransport := options.HTTPTransport
	if upstreamTransport == nil {
		upstreamTransport = http.DefaultTransport.(*http.Transport).Clone()
	}
	relayHandler := &CodexTrafficRelayHandler{
		taskRouter:        options.TaskRouter,
		completionLedger:  options.CompletionLedger,
		requestCounters:   options.RequestCounters,
		upstreamOrigin:    options.UpstreamOrigin,
		upstreamTransport: upstreamTransport,
		drainComplete:     make(chan struct{}),
		openWebSockets:    make(map[*terminalResponseObserver]struct{}),
	}
	relayHandler.reverseProxy = relayHandler.createBytePreservingReverseProxy()
	relayHandler.authenticatedHandler = clientcapability.RequirePathPrefix(options.ClientCapability, http.HandlerFunc(relayHandler.serveAuthenticatedCodexTraffic))
	return relayHandler, nil
}

// ServeHTTP authenticates the private client path before any account credential is selected.
func (handler *CodexTrafficRelayHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	requestWithTrailerSource := request.WithContext(context.WithValue(
		request.Context(), requestTrailerSourceContextKey{}, request.Trailer,
	))
	handler.authenticatedHandler.ServeHTTP(response, requestWithTrailerSource)
}

func (handler *CodexTrafficRelayHandler) serveAuthenticatedCodexTraffic(response http.ResponseWriter, request *http.Request) {
	handler.beginActiveRelayRequest()
	defer handler.finishActiveRelayRequest()

	codexTaskID := taskIdentityForRequest(request)
	workKind := workKindForRequest(request)
	client := clientNameForRequest(request)
	var modelSlug string
	var responseStreamRequested bool
	var warmup bool
	if workKind == store.ModelTurnWorkKind && request.Body != nil {
		body, err := io.ReadAll(request.Body)
		request.Body.Close()
		if err != nil {
			log.Printf("%s: reading the model request from the client failed: %v", describeModelRequest(codexTaskID, client, ""), err)
			http.Error(response, "read model request", http.StatusBadRequest)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		inspectedBody := body
		if strings.EqualFold(strings.TrimSpace(request.Header.Get("Content-Encoding")), "zstd") {
			decoder, err := zstd.NewReader(nil)
			if err == nil {
				inspectedBody, err = decoder.DecodeAll(body, nil)
				decoder.Close()
			}
			if err != nil {
				log.Printf("%s: the zstd-compressed model request could not be decoded: %v", describeModelRequest(codexTaskID, client, ""), err)
				http.Error(response, "decode model request", http.StatusBadRequest)
				return
			}
		}
		var modelRequest struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Generate *bool  `json:"generate"`
		}
		if json.Unmarshal(inspectedBody, &modelRequest) == nil {
			modelSlug = modelRequest.Model
			responseStreamRequested = modelRequest.Stream
			warmup = modelRequest.Generate != nil && !*modelRequest.Generate
		}
	}
	var routeDecision routing.RouteDecision
	var err error
	if request.Method == http.MethodGet && request.URL.Path == "/backend-api/codex/models" {
		routeDecision, err = handler.taskRouter.RouteForModelCatalogue()
	} else {
		routeDecision, err = handler.taskRouter.RouteForTask(codexTaskID, workKind, modelSlug)
	}
	if workKind == store.ModelTurnWorkKind && errors.Is(err, routing.ErrNoAvailableAccount) {
		waitStarted := time.Now()
		log.Printf("%s waits before sending %s to OpenAI: %v", describeModelRequest(codexTaskID, client, ""), modelSlug, err)
		recheck := time.NewTicker(5 * time.Second)
		defer recheck.Stop()
		for errors.Is(err, routing.ErrNoAvailableAccount) {
			select {
			case <-request.Context().Done():
				log.Printf("%s stopped waiting for an account after %s: the client closed the request", describeModelRequest(codexTaskID, client, ""), time.Since(waitStarted).Round(time.Second))
				return
			case <-recheck.C:
				routeDecision, err = handler.taskRouter.RouteForTask(codexTaskID, workKind, modelSlug)
			}
		}
		if err == nil {
			log.Printf("%s stopped waiting after %s", describeModelRequest(codexTaskID, client, routeDecision.AccountName), time.Since(waitStarted).Round(time.Second))
		}
	}
	if err != nil {
		log.Printf("%s could not be routed: %v", describeModelRequest(codexTaskID, client, ""), err)
		http.Error(response, "codex-switch could not choose an account for this request; see the codex-switch proxy log", http.StatusServiceUnavailable)
		return
	}
	if routeDecision.Assignment != "" {
		log.Printf("%s: %s", describeModelRequest(codexTaskID, client, ""), routeDecision.Assignment)
	}

	requestTrailerSource, _ := request.Context().Value(requestTrailerSourceContextKey{}).(http.Header)
	routedRequest := &routedRelayRequest{
		codexTaskID: codexTaskID, client: client, workKind: workKind, modelSlug: modelSlug, responseStreamRequested: responseStreamRequested, routeDecision: routeDecision,
		requestTrailerSource: requestTrailerSource,
	}
	forwardedRequest := request.Clone(context.WithValue(request.Context(), relayRequestContextKey{}, routedRequest))
	if workKind == store.ModelTurnWorkKind {
		observer := newTerminalResponseObserver(codexTaskID, client, routeDecision.AccountName, handler.taskRouter, handler.completionLedger)
		observer.modelSlug = modelSlug
		observer.requestCounters = handler.requestCounters
		routedRequest.responseObserver = observer
		if request.Method == http.MethodPost {
			observer.startModelRequest(warmup)
			observer.admittedRequests = 1
		}
		defer observer.finishModelRequests()
		defer handler.forgetOpenWebSocket(observer)
	}
	handler.reverseProxy.ServeHTTP(response, forwardedRequest)
}

func (handler *CodexTrafficRelayHandler) createBytePreservingReverseProxy() *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			routedRequest := proxyRequest.In.Context().Value(relayRequestContextKey{}).(*routedRelayRequest)
			proxyRequest.SetURL(handler.upstreamOrigin)
			proxyRequest.Out.Host = handler.upstreamOrigin.Host
			proxyRequest.Out.Header.Del("Accept-Encoding")
			if proxyRequest.Out.Body != nil && len(proxyRequest.Out.Trailer) > 0 {
				proxyRequest.Out.Body = &requestTrailerBody{
					ReadCloser:  proxyRequest.Out.Body,
					source:      routedRequest.requestTrailerSource,
					destination: proxyRequest.Out.Trailer,
				}
			}
			proxyRequest.Out.Header.Set("Authorization", "Bearer "+routedRequest.routeDecision.Credentials.AccessToken)
			proxyRequest.Out.Header.Set(chatGPTAccountIDHeader, routedRequest.routeDecision.Credentials.AccountID)
		},
		Transport: handler.upstreamTransport,
		ModifyResponse: func(upstreamResponse *http.Response) error {
			routedRequest := upstreamResponse.Request.Context().Value(relayRequestContextKey{}).(*routedRelayRequest)
			if upstreamResponse.StatusCode >= http.StatusBadRequest {
				return handler.observeRejectedResponse(routedRequest, upstreamResponse)
			}
			if routedRequest.workKind != store.ModelTurnWorkKind {
				return nil
			}
			responseObserver := routedRequest.responseObserver

			if upstreamResponse.StatusCode == http.StatusSwitchingProtocols {
				upgradedConnection, ok := upstreamResponse.Body.(io.ReadWriteCloser)
				if !ok {
					return errors.New("upstream WebSocket connection cannot be relayed")
				}
				observedConnection := createObservedWebSocketConnection(
					upgradedConnection,
					upstreamResponse.Header.Get("Sec-WebSocket-Extensions"),
					responseObserver.observeStructuredEvent,
				)
				observedConnection.observeClientMessages(responseObserver.routeClientMessage)
				observedConnection.reportReadError = responseObserver.logReadError
				upstreamResponse.Body = observedConnection
				handler.trackOpenWebSocket(responseObserver, func() { observedConnection.Close() })
				return nil
			}

			contentType := strings.ToLower(upstreamResponse.Header.Get("Content-Type"))
			if strings.HasPrefix(contentType, "text/event-stream") || (routedRequest.responseStreamRequested && upstreamResponse.StatusCode == http.StatusOK) {
				observedBody := createObservedResponseBody(
					upstreamResponse.Body,
					createServerSentEventObserver(responseObserver.observeStructuredEvent),
				)
				observedBody.reportReadError = responseObserver.logReadError
				upstreamResponse.Body = observedBody
				return nil
			}
			if strings.HasPrefix(contentType, "application/json") {
				observedBody := createObservedResponseBody(
					upstreamResponse.Body,
					createJSONResponseObserver(responseObserver.observeHTTPJSONBody),
				)
				observedBody.reportReadError = responseObserver.logReadError
				upstreamResponse.Body = observedBody
			}
			return nil
		},
		ErrorHandler: func(response http.ResponseWriter, request *http.Request, err error) {
			routedRequest := request.Context().Value(relayRequestContextKey{}).(*routedRelayRequest)
			subject := describeModelRequest(routedRequest.codexTaskID, routedRequest.client, routedRequest.routeDecision.AccountName)
			switch {
			case errors.Is(err, errUpstreamRejectedBeforeAdmission):
				// Logged when the rejection was observed.
			case request.Context().Err() != nil:
				// A model POST counts as interrupted; a cancelled metadata read or
				// WebSocket handshake carries no model work.
				if routedRequest.workKind == store.ModelTurnWorkKind && request.Method == http.MethodPost {
					log.Printf("%s: the client closed the %s request before OpenAI answered", subject, routedRequest.modelSlug)
				}
			default:
				log.Printf("%s could not reach OpenAI: %v", subject, err)
			}
			http.Error(response, "upstream service unavailable", http.StatusBadGateway)
		},
		FlushInterval: -1,
	}
}

// ReverseProxy clones request trailers before the inbound body has populated their final values.
type requestTrailerBody struct {
	io.ReadCloser
	source      http.Header
	destination http.Header
}

func (body *requestTrailerBody) Read(buffer []byte) (int, error) {
	bytesRead, err := body.ReadCloser.Read(buffer)
	if errors.Is(err, io.EOF) {
		for headerName, values := range body.source {
			body.destination[headerName] = append([]string(nil), values...)
		}
	}
	return bytesRead, err
}

func workKindForRequest(request *http.Request) store.WorkKind {
	if request.URL.Path == codexResponsesPath || request.URL.Path == codexCompactResponsesPath {
		return store.ModelTurnWorkKind
	}
	return store.TaskMetadataWorkKind
}

func (handler *CodexTrafficRelayHandler) beginActiveRelayRequest() {
	handler.drainMutex.Lock()
	handler.activeRequestCount++
	handler.drainMutex.Unlock()
}

func (handler *CodexTrafficRelayHandler) finishActiveRelayRequest() {
	handler.drainMutex.Lock()
	handler.activeRequestCount--
	if handler.drainStarted && handler.activeRequestCount == 0 {
		select {
		case <-handler.drainComplete:
		default:
			close(handler.drainComplete)
		}
	}
	handler.drainMutex.Unlock()
}

func (handler *CodexTrafficRelayHandler) closeIdleUpstreamConnections() {
	if idleConnectionCloser, ok := handler.upstreamTransport.(interface{ CloseIdleConnections() }); ok {
		idleConnectionCloser.CloseIdleConnections()
	}
}

var _ http.Handler = (*CodexTrafficRelayHandler)(nil)
