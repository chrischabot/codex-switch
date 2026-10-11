package codexstate

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAIBaseURLReadsEffectiveAppServerConfig(t *testing.T) {
	server := startAppServerFixture(t, nil)
	defer server.close()
	server.config = map[string]any{
		"model":           "gpt-5.6-sol",
		"openai_base_url": "http://127.0.0.1:2455/backend-api/codex",
	}

	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	got, err := reader.ModelConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "openai" || got.OpenAIBaseURL == nil || *got.OpenAIBaseURL != "http://127.0.0.1:2455/backend-api/codex" {
		t.Fatalf("model config = %#v", got)
	}
	server.assertConfigReadOnly(t)
}

func TestOpenAIBaseURLAbsentMeansStandardEndpoint(t *testing.T) {
	server := startAppServerFixture(t, nil)
	defer server.close()
	server.config = map[string]any{"model": "gpt-5.6-sol"}

	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	got, err := reader.ModelConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "openai" || got.OpenAIBaseURL != nil {
		t.Fatalf("model config = %#v, want standard OpenAI", got)
	}
}

func TestOpenAIBaseURLFailsClosedWhenEffectiveConfigIsMissing(t *testing.T) {
	server := startAppServerFixture(t, nil)
	defer server.close()

	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	if _, err := reader.ModelConfig(t.Context()); err == nil {
		t.Fatal("missing effective config was treated as the standard endpoint")
	}
}

func TestModelConfigReportsNonOpenAIProvider(t *testing.T) {
	server := startAppServerFixture(t, nil)
	defer server.close()
	server.config = map[string]any{
		"model_provider":  "gateway",
		"openai_base_url": "http://127.0.0.1:2455/backend-api/codex",
	}

	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	got, err := reader.ModelConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "gateway" {
		t.Fatalf("model config = %#v", got)
	}
}

func TestModelConfigFailsClosedOnExplicitEmptyProvider(t *testing.T) {
	server := startAppServerFixture(t, nil)
	defer server.close()
	server.config = map[string]any{"model_provider": ""}

	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	if _, err := reader.ModelConfig(t.Context()); err == nil {
		t.Fatal("empty model provider was treated as built-in OpenAI")
	}
}

func TestModelConfigFallsBackToOfficialOneShotReader(t *testing.T) {
	var command *exec.Cmd
	reader := LocalReader{
		AppServerSocket:  filepath.Join(t.TempDir(), "missing.sock"),
		AppServerTimeout: 10 * time.Millisecond,
		configCommand: func(ctx context.Context) *exec.Cmd {
			command = exec.CommandContext(ctx, os.Args[0], "-test.run=TestModelConfigProcessHelper")
			command.Env = append(os.Environ(), "CODEX_SWITCH_CONFIG_HELPER=success-block")
			return command
		},
	}
	got, err := reader.ModelConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "openai" || got.OpenAIBaseURL == nil || *got.OpenAIBaseURL != "http://127.0.0.1:2455/backend-api/codex" {
		t.Fatalf("model config = %#v", got)
	}
	if command == nil || command.ProcessState == nil {
		t.Fatal("one-shot reader was not reaped")
	}
}

func TestModelConfigReportsBothReadFailures(t *testing.T) {
	reader := LocalReader{
		AppServerSocket:  filepath.Join(t.TempDir(), "missing.sock"),
		AppServerTimeout: 10 * time.Millisecond,
		configCommand: func(ctx context.Context) *exec.Cmd {
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestModelConfigProcessHelper")
			cmd.Env = append(os.Environ(), "CODEX_SWITCH_CONFIG_HELPER=fail")
			return cmd
		},
	}
	if _, err := reader.ModelConfig(t.Context()); err == nil {
		t.Fatal("both unavailable readers were treated as effective config")
	}
}

func TestModelConfigTimeoutReapsOneShotReader(t *testing.T) {
	var command *exec.Cmd
	reader := LocalReader{
		AppServerSocket:  filepath.Join(t.TempDir(), "missing.sock"),
		AppServerTimeout: 10 * time.Millisecond,
		configCommand: func(ctx context.Context) *exec.Cmd {
			command = exec.CommandContext(ctx, os.Args[0], "-test.run=TestModelConfigProcessHelper")
			command.Env = append(os.Environ(), "CODEX_SWITCH_CONFIG_HELPER=block-before-response")
			return command
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := reader.ModelConfig(ctx); err == nil {
		t.Fatal("blocked one-shot reader did not time out")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
	if command == nil || command.ProcessState == nil {
		t.Fatal("timed-out one-shot reader was not reaped")
	}
}

func TestModelConfigProcessHelper(t *testing.T) {
	mode := os.Getenv("CODEX_SWITCH_CONFIG_HELPER")
	if mode == "" {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	var request rpcRequest
	if err := decoder.Decode(&request); err != nil {
		os.Exit(2)
	}
	if mode == "fail" {
		os.Exit(3)
	}
	if mode == "block-before-response" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if request.Method != "initialize" {
		os.Exit(4)
	}
	if err := encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{}}); err != nil {
		os.Exit(5)
	}
	if err := decoder.Decode(&request); err != nil || request.Method != "initialized" {
		os.Exit(6)
	}
	if err := decoder.Decode(&request); err != nil || request.Method != "config/read" {
		os.Exit(7)
	}
	if err := encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"config": map[string]any{
		"model_provider": "openai", "openai_base_url": "http://127.0.0.1:2455/backend-api/codex",
	}}}); err != nil {
		os.Exit(8)
	}
	if mode == "success-block" {
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(0)
}

func (f *appServerFixture) assertConfigReadOnly(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	requests := append([]fixtureRequest(nil), f.requests...)
	f.mu.Unlock()
	if len(requests) != 3 || requests[0].Method != "initialize" ||
		requests[1].Method != "initialized" || requests[2].Method != "config/read" {
		t.Fatalf("requests = %#v", requests)
	}
	var params map[string]any
	if err := json.Unmarshal(requests[2].Params, &params); err != nil || len(params) != 0 {
		t.Fatalf("config/read params = %s", requests[2].Params)
	}
}
