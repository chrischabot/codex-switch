// continuity-upstream is an isolated development gateway, never a live proxy.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

func main() {
	dataDirectory := flag.String("data-dir", "", "isolated proxy data directory containing account-N/auth.json")
	controlPath := flag.String("control", "", "simulation file: http-only, hold-body PATH, delay-metadata ACCOUNT DURATION, reject-model ACCOUNT MODEL, limited ACCOUNT, reject ACCOUNT, or reject-after ACCOUNT N; empty restores real upstream")
	flag.Parse()
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC)
	if *dataDirectory == "" || *controlPath == "" {
		log.Fatal("data-dir and control are required")
	}
	accountAliases, err := loadAccountAliases(*dataDirectory)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := readSimulationControls(*controlPath, accountAliases); err != nil {
		log.Fatal(err)
	}

	upstream := &url.URL{Scheme: "https", Host: "chatgpt.com"}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	gateway := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(upstream)
			if request.In.URL.Path == "/backend-api/wham/usage" {
				request.Out.Header.Del("Accept-Encoding")
			}
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorLog:      log.New(io.Discard, "", 0),
		ModifyResponse: func(response *http.Response) error {
			logRequest("response", "upstream", response.Request, accountAliases, response.StatusCode)
			if turnState := response.Header.Get("X-Codex-Turn-State"); turnState != "" {
				log.Printf("event=response-turn-state account=%q thread=%q turn_state=%s",
					accountAliases[response.Request.Header.Get("ChatGPT-Account-ID")], response.Request.Header.Get("Thread-ID"), shortHash(turnState))
			}
			return holdResponseBody(response, *controlPath, accountAliases)
		},
		ErrorHandler: func(writer http.ResponseWriter, request *http.Request, _ error) {
			logRequest("transport-error", "upstream", request, accountAliases, http.StatusBadGateway)
			http.Error(writer, "upstream transport failed", http.StatusBadGateway)
		},
	}
	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			simulationControls, err := readSimulationControls(*controlPath, accountAliases)
			if err != nil {
				logRequest("control-error", "simulation", request, accountAliases, http.StatusServiceUnavailable)
				http.Error(writer, "invalid simulation control file", http.StatusServiceUnavailable)
				return
			}
			if simulationControls.httpOnly && request.Method == http.MethodGet && request.URL.Path == "/backend-api/codex/responses" && strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
				logRequest("websocket-disabled-before-admission", "simulation", request, accountAliases, http.StatusUpgradeRequired)
				http.Error(writer, "WebSocket disabled (isolated simulation)", http.StatusUpgradeRequired)
				return
			}
			alias := accountAliases[request.Header.Get("ChatGPT-Account-ID")]
			if delay := simulationControls.metadataDelays[alias]; delay > 0 && (request.URL.Path == "/backend-api/wham/usage" || request.URL.Path == "/backend-api/codex/models") {
				logRequest("metadata-delay", "simulation", request, accountAliases, 0)
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-request.Context().Done():
					return
				}
			}
			control := simulationControls.accounts[alias]
			if control.mode == simulateUnsupportedModel && request.URL.Path == "/backend-api/codex/responses" {
				logRequest("unsupported-model-before-admission", "simulation", request, accountAliases, http.StatusBadRequest)
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(writer).Encode(map[string]string{"detail": "The '" + control.rejectedModelSlug + "' model is not supported when using Codex with a ChatGPT account."})
				return
			}
			if control.mode == simulateLimitedAllowance || control.mode == simulateModelRejection {
				resetAt := time.Now().Add(time.Hour).Unix()
				switch request.URL.Path {
				case "/backend-api/wham/usage":
					if control.mode != simulateLimitedAllowance {
						break
					}
					logRequest("response", "simulation", request, accountAliases, http.StatusOK)
					writer.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(writer, `{"plan_type":"pro","rate_limit":{"allowed":false,"limit_reached":true,"primary_window":{"used_percent":100,"limit_window_seconds":18000,"reset_after_seconds":3600,"reset_at":%d}}}`, resetAt)
					return
				case "/backend-api/codex/responses":
					logRequest("rejected-before-admission", "simulation", request, accountAliases, http.StatusTooManyRequests)
					writer.Header().Set("Content-Type", "application/json")
					writer.WriteHeader(http.StatusTooManyRequests)
					fmt.Fprintf(writer, `{"error":{"type":"usage_limit_reached","message":"Usage limit reached (isolated simulation)","plan_type":"pro","resets_at":%d}}`, resetAt)
					return
				}
			}
			if control.mode == simulateRejectionAfterMessages && request.URL.Path == "/backend-api/codex/responses" && strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
				relaySimulatedWebSocket(writer, request, upstream, transport, control.acceptedMessages, *controlPath, accountAliases)
				return
			}
			logRequest("admission", "upstream", request, accountAliases, 0)
			gateway.ServeHTTP(writer, request)
		}),
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal("cannot listen on loopback")
	}
	fmt.Printf("http://%s\n", listener.Addr())
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("gateway server stopped unexpectedly")
	}
}

func logRequest(event, source string, request *http.Request, accountAliases map[string]string, status int) {
	alias := accountAliases[request.Header.Get("ChatGPT-Account-ID")]
	if alias == "" {
		alias = "unknown"
	}
	log.Printf("event=%s source=%s method=%q path=%q account=%q thread=%q session=%s subagent=%q turn_state=%s status=%d remote=%q",
		event, source, request.Method, request.URL.Path, alias, request.Header.Get("Thread-ID"),
		shortHash(request.Header.Get("Session-ID")), request.Header.Get("X-OpenAI-Subagent"),
		shortHash(request.Header.Get("X-Codex-Turn-State")), status, request.RemoteAddr)
}

// shortHash identifies opaque server tokens in logs without recording them.
func shortHash(value string) string {
	if value == "" {
		return "-"
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:4])
}

// logWebSocketTurnState records the turn-state token a WebSocket message
// carries: Codex sends it in response.create client_metadata, and the server
// returns it in response.metadata headers.
func logWebSocketTurnState(direction string, request *http.Request, accountAliases map[string]string, payload []byte) {
	var event struct {
		Type           string            `json:"type"`
		ClientMetadata map[string]string `json:"client_metadata"`
		Headers        map[string]any    `json:"headers"`
		Error          json.RawMessage   `json:"error"`
		Response       struct {
			Error json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(payload, &event) != nil {
		return
	}
	turnState := event.ClientMetadata["x-codex-turn-state"]
	for name, value := range event.Headers {
		if text, ok := value.(string); ok && strings.EqualFold(name, "x-codex-turn-state") {
			turnState = text
		}
	}
	switch {
	case event.Type == "error" || event.Type == "response.failed":
		failure := event.Error
		if len(failure) == 0 {
			failure = event.Response.Error
		}
		log.Printf("event=websocket-%s type=%s account=%q thread=%q error=%s", direction, event.Type,
			accountAliases[request.Header.Get("ChatGPT-Account-ID")], request.Header.Get("Thread-ID"), failure)
	case turnState != "" || event.Type == "response.create":
		log.Printf("event=websocket-%s type=%s account=%q thread=%q turn_state=%s", direction, event.Type,
			accountAliases[request.Header.Get("ChatGPT-Account-ID")], request.Header.Get("Thread-ID"), shortHash(turnState))
	}
}

func loadAccountAliases(dataDirectory string) (map[string]string, error) {
	paths, err := filepath.Glob(filepath.Join(dataDirectory, "account-*", "auth.json"))
	if err != nil || len(paths) == 0 {
		return nil, errors.New("no isolated account auth files found")
	}
	aliases := make(map[string]string, len(paths))
	for _, path := range paths {
		alias := filepath.Base(filepath.Dir(path))
		if number, err := strconv.Atoi(strings.TrimPrefix(alias, "account-")); err != nil || number < 1 {
			return nil, errors.New("account directory must be named account-N")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("cannot read auth for %s", alias)
		}
		var auth struct {
			Tokens struct {
				AccountID string `json:"account_id"`
			} `json:"tokens"`
		}
		if json.Unmarshal(data, &auth) != nil || auth.Tokens.AccountID == "" {
			return nil, fmt.Errorf("auth for %s has no valid account ID", alias)
		}
		if _, exists := aliases[auth.Tokens.AccountID]; exists {
			return nil, errors.New("isolated auth files contain duplicate accounts")
		}
		aliases[auth.Tokens.AccountID] = alias
	}
	return aliases, nil
}

type simulationMode string

const (
	simulateUnsupportedModel       simulationMode = "reject-model"
	simulateLimitedAllowance       simulationMode = "limited"
	simulateModelRejection         simulationMode = "reject"
	simulateRejectionAfterMessages simulationMode = "reject-after"
)

type simulationControl struct {
	mode              simulationMode
	acceptedMessages  int
	rejectedModelSlug string
}

type simulationConfiguration struct {
	accounts       map[string]simulationControl
	httpOnly       bool
	heldBodies     map[string]bool
	metadataDelays map[string]time.Duration
	weeklyUsed     map[string]float64
}

var simulationControlMu sync.Mutex

func readSimulationControls(path string, accountAliases map[string]string) (*simulationConfiguration, error) {
	simulationControlMu.Lock()
	defer simulationControlMu.Unlock()
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read simulation control file")
	}
	defer file.Close()
	controls := &simulationConfiguration{accounts: make(map[string]simulationControl), heldBodies: make(map[string]bool), metadataDelays: make(map[string]time.Duration), weeklyUsed: make(map[string]float64)}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "http-only" {
			controls.httpOnly = true
			continue
		}
		fields := strings.Fields(line)
		if fields[0] == "hold-body" {
			if len(fields) != 2 || (fields[1] != "/backend-api/codex/responses" && fields[1] != "/backend-api/codex/models") {
				return nil, errors.New("hold-body requires /backend-api/codex/responses or /backend-api/codex/models")
			}
			controls.heldBodies[fields[1]] = true
			continue
		}
		if fields[0] == "weekly-used" {
			if len(fields) != 3 {
				return nil, errors.New("weekly-used requires account alias and percent")
			}
			percent, err := strconv.ParseFloat(fields[2], 64)
			if err != nil || percent < 0 || percent > 100 {
				return nil, errors.New("invalid weekly-used percent")
			}
			controls.weeklyUsed[fields[1]] = percent
			continue
		}
		if fields[0] == "delay-metadata" {
			if len(fields) != 3 {
				return nil, errors.New("delay-metadata requires account alias and duration")
			}
			delay, err := time.ParseDuration(fields[2])
			if err != nil || delay < 0 {
				return nil, errors.New("invalid metadata delay")
			}
			controls.metadataDelays[fields[1]] = delay
			continue
		}
		control := simulationControl{mode: simulationMode(fields[0])}
		switch control.mode {
		case simulateLimitedAllowance, simulateModelRejection:
			if len(fields) != 2 {
				return nil, errors.New("control requires a mode and account alias")
			}
		case simulateUnsupportedModel:
			if len(fields) != 3 {
				return nil, errors.New("reject-model requires account alias and model slug")
			}
			control.rejectedModelSlug = fields[2]
		case simulateRejectionAfterMessages:
			if len(fields) != 3 {
				return nil, errors.New("reject-after requires an account alias and accepted message count")
			}
			count, err := strconv.Atoi(fields[2])
			if err != nil || count < 0 {
				return nil, errors.New("reject-after count must be a non-negative integer")
			}
			control.acceptedMessages = count
		default:
			return nil, errors.New("control accepts http-only, hold-body, delay-metadata, reject-model, limited, reject, or reject-after")
		}
		known := false
		for _, alias := range accountAliases {
			known = known || alias == fields[1]
		}
		if !known {
			return nil, errors.New("control file names an unknown account")
		}
		controls.accounts[fields[1]] = control
	}
	if scanner.Err() != nil {
		return nil, errors.New("cannot read simulation control file")
	}
	return controls, nil
}

// This simulation terminates WebSocket framing with compression disabled. It
// preserves accepted message payloads; the normal path preserves raw frames.
func relaySimulatedWebSocket(writer http.ResponseWriter, request *http.Request, upstream *url.URL, transport http.RoundTripper, acceptedMessages int, controlPath string, accountAliases map[string]string) {
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	upstreamURL := *request.URL
	upstreamURL.Scheme, upstreamURL.Host = upstream.Scheme, upstream.Host
	headers := request.Header.Clone()
	for _, name := range []string{"Connection", "Upgrade", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions", "Sec-WebSocket-Protocol"} {
		headers.Del(name)
	}
	serverConnection, response, err := websocket.Dial(ctx, upstreamURL.String(), &websocket.DialOptions{
		HTTPClient:      &http.Client{Transport: transport},
		HTTPHeader:      headers,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		status := http.StatusBadGateway
		if response != nil {
			status = response.StatusCode
		}
		logRequest("websocket-dial-error", "upstream", request, accountAliases, status)
		http.Error(writer, "simulation websocket upstream handshake failed", status)
		return
	}
	defer serverConnection.CloseNow()
	if turnState := response.Header.Get("X-Codex-Turn-State"); turnState != "" {
		log.Printf("event=websocket-handshake-turn-state account=%q thread=%q turn_state=%s",
			accountAliases[request.Header.Get("ChatGPT-Account-ID")], request.Header.Get("Thread-ID"), shortHash(turnState))
	}
	for name, values := range response.Header {
		if strings.EqualFold(name, "Connection") || strings.EqualFold(name, "Upgrade") || strings.HasPrefix(strings.ToLower(name), "sec-websocket-") {
			continue
		}
		writer.Header()[name] = append([]string(nil), values...)
	}
	clientConnection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer clientConnection.CloseNow()
	serverConnection.SetReadLimit(-1)
	clientConnection.SetReadLimit(-1)
	logRequest("websocket-open", "simulation-relay", request, accountAliases, http.StatusSwitchingProtocols)

	var activityMu sync.Mutex
	activeResponses := 0
	idle := make(chan struct{})
	close(idle)
	finished := make(chan struct{}, 2)
	go func() {
		defer func() { finished <- struct{}{} }()
		for {
			messageType, payload, err := serverConnection.Read(ctx)
			if err != nil {
				return
			}
			logWebSocketTurnState("server", request, accountAliases, payload)
			if err := clientConnection.Write(ctx, messageType, payload); err != nil {
				return
			}
			var event struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(payload, &event) != nil {
				continue
			}
			switch event.Type {
			case "response.completed", "response.failed", "response.incomplete", "response.cancelled", "error":
				activityMu.Lock()
				if activeResponses > 0 {
					activeResponses--
					if activeResponses == 0 {
						close(idle)
					}
				}
				activityMu.Unlock()
			}
		}
	}()
	go func() {
		defer func() { finished <- struct{}{} }()
		forwardedMessages := 0
		for {
			messageType, payload, err := clientConnection.Read(ctx)
			if err != nil {
				return
			}
			logWebSocketTurnState("client", request, accountAliases, payload)
			var event struct {
				Type     string `json:"type"`
				Generate *bool  `json:"generate"`
			}
			modelRequest := json.Unmarshal(payload, &event) == nil && event.Type == "response.create" && (event.Generate == nil || *event.Generate)
			if modelRequest && forwardedMessages >= acceptedMessages {
				activityMu.Lock()
				previousResponsesFinished := idle
				activityMu.Unlock()
				select {
				case <-previousResponsesFinished:
				case <-ctx.Done():
					return
				}
				reject, activated, err := activateSimulatedLimit(controlPath, accountAliases[request.Header.Get("ChatGPT-Account-ID")], acceptedMessages)
				if err != nil {
					logRequest("control-error", "simulation", request, accountAliases, http.StatusServiceUnavailable)
					return
				}
				if activated {
					logRequest("simulation-limit-activated", "simulation", request, accountAliases, http.StatusTooManyRequests)
				}
				if reject {
					logRequest("websocket-rejected-before-admission", "simulation", request, accountAliases, http.StatusTooManyRequests)
					errorEvent := []byte(fmt.Sprintf(`{"type":"error","status":429,"error":{"type":"usage_limit_reached","message":"Usage limit reached (isolated simulation)","plan_type":"pro","resets_at":%d}}`, time.Now().Add(time.Hour).Unix()))
					if clientConnection.Write(ctx, websocket.MessageText, errorEvent) != nil {
						return
					}
					continue
				}
			}
			if modelRequest {
				activityMu.Lock()
				if activeResponses == 0 {
					idle = make(chan struct{})
				}
				activeResponses++
				activityMu.Unlock()
				forwardedMessages++
				logRequest("websocket-message-admission", "upstream", request, accountAliases, 0)
			}
			if serverConnection.Write(ctx, messageType, payload) != nil {
				return
			}
		}
	}()
	<-finished
	cancel()
	clientConnection.CloseNow()
	serverConnection.CloseNow()
	<-finished
}

func activateSimulatedLimit(path, accountAlias string, acceptedMessages int) (reject, activated bool, err error) {
	simulationControlMu.Lock()
	defer simulationControlMu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false, errors.New("cannot read simulation control file")
	}
	lines := strings.Split(string(data), "\n")
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != accountAlias {
			continue
		}
		if fields[0] == string(simulateLimitedAllowance) || fields[0] == string(simulateModelRejection) {
			return true, false, nil
		}
		if len(fields) != 3 || fields[0] != string(simulateRejectionAfterMessages) {
			continue
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil || count != acceptedMessages {
			continue
		}
		lines[index] = "limited " + accountAlias
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0600); err != nil {
			return false, false, errors.New("cannot update simulation control file")
		}
		return true, true, nil
	}
	return false, false, nil
}

func holdResponseBody(response *http.Response, controlPath string, accountAliases map[string]string) error {
	if response.StatusCode == http.StatusSwitchingProtocols {
		return nil
	}
	controls, err := readSimulationControls(controlPath, accountAliases)
	if err != nil {
		return err
	}
	alias := accountAliases[response.Request.Header.Get("ChatGPT-Account-ID")]
	if percent, simulated := controls.weeklyUsed[alias]; simulated && response.Request.URL.Path == "/backend-api/wham/usage" && response.StatusCode == http.StatusOK {
		return rewriteWeeklyUsedPercent(response, percent)
	}
	if controls.heldBodies[response.Request.URL.Path] {
		response.Body = &heldResponseBody{
			ReadCloser: response.Body, request: response.Request, status: response.StatusCode,
			controlPath: controlPath, accountAliases: accountAliases,
		}
	}
	return nil
}

type heldResponseBody struct {
	io.ReadCloser
	request        *http.Request
	status         int
	controlPath    string
	accountAliases map[string]string
	firstByteRead  bool
	released       bool
}

func (body *heldResponseBody) Read(destination []byte) (int, error) {
	if !body.firstByteRead && len(destination) > 0 {
		count, err := body.ReadCloser.Read(destination[:1])
		if count > 0 {
			body.firstByteRead = true
			logRequest("response-first-byte", "upstream", body.request, body.accountAliases, body.status)
		}
		return count, err
	}
	if body.firstByteRead && !body.released {
		logRequest("response-body-held", "simulation", body.request, body.accountAliases, body.status)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			controls, err := readSimulationControls(body.controlPath, body.accountAliases)
			if err != nil {
				return 0, err
			}
			if !controls.heldBodies[body.request.URL.Path] {
				body.released = true
				logRequest("response-body-released", "simulation", body.request, body.accountAliases, body.status)
				break
			}
			select {
			case <-body.request.Context().Done():
				logRequest("response-body-canceled", "simulation", body.request, body.accountAliases, body.status)
				return 0, body.request.Context().Err()
			case <-ticker.C:
			}
		}
	}
	return body.ReadCloser.Read(destination)
}

// rewriteWeeklyUsedPercent simulates a weekly allowance level so routing
// experiments can choose which real account receives new tasks.
func rewriteWeeklyUsedPercent(response *http.Response, percent float64) error {
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return err
	}
	var usage map[string]any
	if err := json.Unmarshal(body, &usage); err != nil {
		return err
	}
	if rateLimit, ok := usage["rate_limit"].(map[string]any); ok {
		for _, name := range []string{"primary_window", "secondary_window"} {
			if window, ok := rateLimit[name].(map[string]any); ok && window["limit_window_seconds"] == float64(7*24*60*60) {
				window["used_percent"] = percent
			}
		}
	}
	body, err = json.Marshal(usage)
	if err != nil {
		return err
	}
	response.Body = io.NopCloser(strings.NewReader(string(body)))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Length", strconv.Itoa(len(body)))
	response.Header.Del("Content-Encoding")
	return nil
}
