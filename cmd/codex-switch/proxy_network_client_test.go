package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt in with a Codex executable: this runs the installed client with temporary
// configuration and synthetic credentials, without any provider model work.
func TestRealCodexClientRecoversAfterProxyConnectionReset(t *testing.T) {
	codex := os.Getenv("CODEX_SWITCH_TEST_CODEX_EXECUTABLE")
	if codex == "" {
		t.Skip("set CODEX_SWITCH_TEST_CODEX_EXECUTABLE to run the installed Codex client")
	}
	testRealClientConnectionReset(t, "Codex", codex)
}

func TestRealPiClientRecoversAfterProxyConnectionReset(t *testing.T) {
	pi := os.Getenv("CODEX_SWITCH_TEST_PI_EXECUTABLE")
	if pi == "" {
		t.Skip("set CODEX_SWITCH_TEST_PI_EXECUTABLE to run the installed Pi client")
	}
	testRealClientConnectionReset(t, "Pi", pi)
}

func TestRealClaudeClientRecoversAfterProxyConnectionReset(t *testing.T) {
	claude := os.Getenv("CODEX_SWITCH_TEST_CLAUDE_EXECUTABLE")
	if claude == "" {
		t.Skip("set CODEX_SWITCH_TEST_CLAUDE_EXECUTABLE to run the installed Claude client")
	}
	testRealClientConnectionReset(t, "Claude", claude)
}

func testRealClientConnectionReset(t *testing.T, clientName, executable string) {
	var modelRequests atomic.Int64
	admitted := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(output http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/oauth/usage" {
			io.WriteString(output, `{"five_hour":{"utilization":0,"resets_at":"2040-01-01T00:00:00Z"},"seven_day":{"utilization":0,"resets_at":"2040-01-01T00:00:00Z"}}`)
			return
		}
		if request.URL.Path == "/backend-api/wham/usage" {
			io.WriteString(output, `{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false}}`)
			return
		}
		if request.URL.Path == "/backend-api/codex/models" {
			io.WriteString(output, `{"models":[{"slug":"gpt-5.6-luna"}]}`)
			return
		}
		if request.URL.Path != "/backend-api/codex/responses" && request.URL.Path != "/v1/messages" {
			http.NotFound(output, request)
			return
		}
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			return
		}
		if modelRequests.Add(1) == 1 {
			admitted <- request.RemoteAddr
			<-request.Context().Done()
			return
		}
		output.Header().Set("Content-Type", "text/event-stream")
		if clientName == "Claude" {
			for _, event := range []string{
				`{"type":"message_start","message":{"id":"message-recovered","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"recovered"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`,
				`{"type":"message_stop"}`,
			} {
				var parsed struct{ Type string }
				json.Unmarshal([]byte(event), &parsed)
				fmt.Fprintf(output, "event: %s\ndata: %s\n\n", parsed.Type, event)
			}
			return
		}
		for _, event := range []string{
			`{"type":"response.created","response":{"id":"response-recovered","status":"in_progress","output":[]}}`,
			`{"type":"response.output_item.added","output_index":0,"item":{"id":"message-recovered","type":"message","role":"assistant","status":"in_progress","content":[]}}`,
			`{"type":"response.content_part.added","item_id":"message-recovered","output_index":0,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}`,
			`{"type":"response.output_text.delta","item_id":"message-recovered","output_index":0,"content_index":0,"delta":"recovered"}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"id":"message-recovered","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"recovered","annotations":[]}]}}`,
			`{"type":"response.completed","response":{"id":"response-recovered","status":"completed","model":"gpt-5.6-luna","output":[{"id":"message-recovered","type":"message","role":"assistant","content":[{"type":"output_text","text":"recovered","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}}`,
		} {
			fmt.Fprintf(output, "data: %s\n\n", event)
		}
	}))
	defer upstream.Close()
	relay := newNetworkFaultTCPRelay(t, upstream.Listener.Addr().String())
	binary := filepath.Join(t.TempDir(), "codex-switch")
	buildTestBinary(t, binary)
	data := t.TempDir()
	if clientName == "Claude" {
		accountDirectory := filepath.Join(data, "account-1")
		if err := os.MkdirAll(accountDirectory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(accountDirectory, "auth.json"), []byte(`{"provider":"claude","access_token":"fixture-access-token","refresh_token":"fixture-refresh-token","expires_at":"2040-01-01T00:00:00Z","account_id":"fixture-account","organization_id":"fixture-organization","email":"client@example.test","plan":"Max 20x"}`), 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		writeAccountFixture(t, filepath.Join(data, "account-1"), "client-user", "client@example.test", processAccessToken("client-user"))
	}
	proxy := startRealBinaryProxy(t, binary, data, "http://"+relay.listener.Addr().String(), "CODEX_HOME="+data, "PI_CODING_AGENT_SESSION_DIR="+data, "CLAUDE_CONFIG_DIR="+data, "CODEX_SWITCH_CLAUDE_UPSTREAM_ORIGIN=http://"+relay.listener.Addr().String())
	clientHome := t.TempDir()
	config := fmt.Sprintf(`model = "gpt-5.6-luna"
model_provider = "fixture"
cli_auth_credentials_store = "file"
[analytics]
enabled = false
[model_providers.fixture]
name = "Local fault proof"
base_url = "http://%s/backend-api/codex"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
request_max_retries = 1
stream_max_retries = 0
`, proxy.authenticatedAddress())
	if err := os.WriteFile(filepath.Join(clientHome, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	var command *exec.Cmd
	environment := []string{"HOME=" + clientHome, "PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.TempDir()}
	switch clientName {
	case "Codex":
		command = exec.CommandContext(ctx, executable, "exec", "--json", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check", "Reply with recovered.")
		environment = append(environment, "CODEX_HOME="+clientHome)
	case "Pi":
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"fixture-account"}}`))
		apiKey := "eyJhbGciOiJub25lIn0." + payload + ".fixture"
		models := fmt.Sprintf(`{"providers":{"openai-codex":{"baseUrl":"http://%s/backend-api/codex","apiKey":%q,"models":[{"id":"gpt-5.6-luna","api":"openai-codex-responses","reasoning":false,"contextWindow":128000,"maxTokens":1000}]}}}`, proxy.authenticatedAddress(), apiKey)
		for name, content := range map[string]string{"models.json": models, "settings.json": `{"transport":"sse","retry":{"enabled":true,"maxRetries":1,"baseDelayMs":10}}`} {
			if err := os.WriteFile(filepath.Join(clientHome, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		command = exec.CommandContext(ctx, executable, "--provider", "openai-codex", "--model", "gpt-5.6-luna", "--api-key", apiKey, "--offline", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--no-session", "--mode", "json", "--print", "Reply with recovered.")
		environment = append(environment, "PI_CODING_AGENT_DIR="+clientHome)
	case "Claude":
		command = exec.CommandContext(ctx, executable, "--bare", "--no-session-persistence", "--output-format", "stream-json", "--verbose", "--model", "claude-haiku-4-5-20251001", "--print", "Reply with recovered.")
		environment = append(environment, "CLAUDE_CODE_SIMPLE=1", "CLAUDE_CONFIG_DIR="+clientHome, "CLAUDE_SECURE_STORAGE_CONFIG_DIR="+clientHome,
			"ANTHROPIC_API_KEY=sk-ant-fixture", "CLAUDE_CODE_MAX_RETRIES=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
			"HTTPS_PROXY=http://codex-switch:"+proxy.clientCapability+"@"+proxy.address,
			"HTTP_PROXY=http://codex-switch:"+proxy.clientCapability+"@"+proxy.address,
			"NODE_EXTRA_CA_CERTS="+filepath.Join(data, "claude-proxy-ca.pem"))
	}
	command.Dir = clientHome
	command.Env = environment
	type clientResult struct {
		output []byte
		err    error
	}
	completed := make(chan clientResult, 1)
	go func() { output, err := command.CombinedOutput(); completed <- clientResult{output, err} }()
	select {
	case address := <-admitted:
		relay.reset(t, address)
	case result := <-completed:
		t.Fatalf("%s stopped before its request reached the provider: %v\n%s", clientName, result.err, result.output)
	case <-ctx.Done():
		t.Fatalf("%s did not send a model request before its deadline", clientName)
	}
	result := <-completed
	recoveryEvent := map[string]string{
		"Codex":  `"type":"turn.completed"`,
		"Pi":     `"type":"auto_retry_end","success":true`,
		"Claude": `"subtype":"success"`,
	}[clientName]
	if result.err != nil || !strings.Contains(string(result.output), `"text":"recovered"`) ||
		!strings.Contains(string(result.output), recoveryEvent) || modelRequests.Load() != 2 {
		t.Fatalf("%s recovery: %v, %d provider requests\n%s", clientName, result.err, modelRequests.Load(), result.output)
	}
	proxy.stop(t)
	proxyLog, err := os.ReadFile(proxyLogPath(data))
	if err != nil || !strings.Contains(string(proxyLog), "the proxy did not resend this attempt") {
		t.Fatalf("proxy did not explain the no-replay decision: %v\n%s", err, proxyLog)
	}
	t.Logf("%s recovered after proxy 502; provider requests: %d; terminal event: %s\n%s", clientName, modelRequests.Load(), recoveryEvent, proxyLog)
}
