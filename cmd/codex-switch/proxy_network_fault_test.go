package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The built proxy process talks to the local provider through a TCP relay.
// Reset a socket only after the provider has received the complete model POST.
func TestProxyProcessNetworkFaults(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "codex-switch")
	buildTestBinary(t, binary)
	admitted := make(chan networkFaultAdmission, 8)
	releases := map[string]chan struct{}{"broken": make(chan struct{}), "healthy": make(chan struct{})}
	upstream := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/backend-api/wham/usage" {
			output.Header().Set("Content-Type", "application/json")
			io.WriteString(output, `{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false}}`)
			return
		}
		var input struct{ Input string }
		if request.URL.Path != "/backend-api/codex/responses" || json.NewDecoder(request.Body).Decode(&input) != nil {
			http.Error(output, "invalid request", http.StatusBadRequest)
			return
		}
		admitted <- networkFaultAdmission{marker: input.Input, connection: request.RemoteAddr, protocol: request.Proto}
		select {
		case <-releases[input.Input]:
		case <-request.Context().Done():
			return
		}
		output.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(output, `{"id":"response-%s","object":"response","status":"completed","model":"gpt-5.6-luna","output":[]}`, input.Input)
	}))
	t.Cleanup(upstream.Close)
	relay := newNetworkFaultTCPRelay(t, upstream.Listener.Addr().String())
	data := t.TempDir()
	writeAccountFixture(t, filepath.Join(data, "account-1"), "network-user", "network@example.test", processAccessToken("network-user"))
	proxy := startRealBinaryProxy(t, binary, data, "http://"+relay.listener.Addr().String(),
		"CODEX_HOME="+data, "PI_CODING_AGENT_SESSION_DIR="+data, "CLAUDE_CONFIG_DIR="+data)
	results := make(chan networkFaultResponse, 2)
	for _, marker := range []string{"broken", "healthy"} {
		go func() { results <- sendNetworkFaultTurn(proxy.authenticatedAddress(), marker) }()
	}
	facts := map[string]networkFaultAdmission{}
	for range 2 {
		fact := waitNetworkFaultAdmission(t, admitted)
		facts[fact.marker] = fact
		if fact.protocol != "HTTP/1.1" {
			t.Fatalf("model request did not use a separate HTTP/1.1 connection: %#v", fact)
		}
	}
	if facts["broken"].connection == facts["healthy"].connection {
		t.Fatal("concurrent sessions share an upstream socket")
	}
	relay.reset(t, facts["broken"].connection)
	close(releases["healthy"])
	for range 2 {
		select {
		case result := <-results:
			want := http.StatusOK
			if result.marker == "broken" {
				want = http.StatusBadGateway
			}
			if result.err != nil || result.status != want {
				t.Fatalf("request after in-flight reset: %#v, want status %d", result, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("clients did not receive their network fault results")
		}
	}
	close(releases["broken"])
	select {
	case duplicate := <-admitted:
		t.Fatalf("proxy replayed model work: %#v", duplicate)
	case <-time.After(100 * time.Millisecond):
	}
	proxy.stop(t)
	logBytes, err := os.ReadFile(proxyLogPath(data))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logBytes), "the proxy did not resend this attempt") {
		t.Fatalf("uncertain reset did not explain its no-replay decision: %s", logBytes)
	}
	t.Log(strings.TrimSpace(string(logBytes)))
}

type networkFaultAdmission struct{ marker, connection, protocol string }

type networkFaultResponse struct {
	marker string
	status int
	body   string
	err    error
}

func sendNetworkFaultTurn(address, marker string) networkFaultResponse {
	body, _ := json.Marshal(map[string]any{"input": marker, "stream": false})
	request, err := http.NewRequest(http.MethodPost, "http://"+address+"/backend-api/codex/responses", bytes.NewReader(body))
	if err != nil {
		return networkFaultResponse{marker: marker, err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Thread-ID", marker)
	request.Header.Set("Idempotency-Key", "network-fixture")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return networkFaultResponse{marker: marker, err: err}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	return networkFaultResponse{marker: marker, status: response.StatusCode, body: string(responseBody), err: err}
}

func waitNetworkFaultAdmission(t *testing.T, admitted <-chan networkFaultAdmission) networkFaultAdmission {
	t.Helper()
	select {
	case fact := <-admitted:
		return fact
	case <-time.After(10 * time.Second):
		t.Fatal("provider did not receive the model request")
		return networkFaultAdmission{}
	}
}

type networkFaultTCPRelay struct {
	listener net.Listener
	mu       sync.Mutex
	sockets  map[string][2]*net.TCPConn
}

func newNetworkFaultTCPRelay(t *testing.T, upstreamAddress string) *networkFaultTCPRelay {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relay := &networkFaultTCPRelay{listener: listener, sockets: make(map[string][2]*net.TCPConn)}
	t.Cleanup(func() {
		listener.Close()
		relay.mu.Lock()
		defer relay.mu.Unlock()
		for _, pair := range relay.sockets {
			pair[0].Close()
			pair[1].Close()
		}
	})
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			incoming := connection.(*net.TCPConn)
			go func() {
				outgoingConnection, err := net.Dial("tcp", upstreamAddress)
				if err != nil {
					incoming.Close()
					return
				}
				outgoing := outgoingConnection.(*net.TCPConn)
				relay.mu.Lock()
				relay.sockets[outgoing.LocalAddr().String()] = [2]*net.TCPConn{incoming, outgoing}
				relay.mu.Unlock()
				defer incoming.Close()
				defer outgoing.Close()
				go func() { io.Copy(outgoing, incoming); outgoing.CloseWrite() }()
				io.Copy(incoming, outgoing)
			}()
		}
	}()
	return relay
}

func (relay *networkFaultTCPRelay) reset(t *testing.T, upstreamRemoteAddress string) {
	t.Helper()
	relay.mu.Lock()
	pair, found := relay.sockets[upstreamRemoteAddress]
	relay.mu.Unlock()
	if !found {
		t.Fatalf("relay cannot find provider socket %s", upstreamRemoteAddress)
	}
	for _, connection := range pair {
		connection.SetLinger(0)
		connection.Close()
	}
}
