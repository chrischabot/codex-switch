package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshp123/codex-switch/internal/clientcapability"
)

// This runs the shipping deployment command and real proxy generations. The
// local providers validate synthetic credentials; no real login is read.
func TestDeployProcessChecksProvidersBeforeTakingTraffic(t *testing.T) {
	root := t.TempDir()
	binary, stable, data := filepath.Join(root, "candidate"), filepath.Join(root, "stable"), filepath.Join(root, "data")
	build := exec.Command("../../scripts/dev/build-proxy", binary)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build proxy: %v\n%s", err, output)
	}
	copyBinary, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stable, copyBinary, 0700); err != nil {
		t.Fatal(err)
	}
	writeAccountFixture(t, filepath.Join(data, "account-1"), "deploy-user", "deploy@example.test", processAccessToken("deploy-user"))
	writeAccountFixture(t, filepath.Join(data, "account-3"), "signed-out", "signed-out@example.test", "")
	claudeDirectory := filepath.Join(data, "account-2")
	if err := os.MkdirAll(claudeDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	claudeAuth := fmt.Sprintf(`{"provider":"claude","access_token":"fixture-claude","refresh_token":"fixture-refresh","expires_at":%q,"account_id":"fixture-account","organization_id":"fixture-organization","email":"claude@example.test"}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(claudeDirectory, "auth.json"), []byte(claudeAuth), 0600); err != nil {
		t.Fatal(err)
	}
	// The incumbent cached a good Claude read. A candidate must perform its own
	// read even when the shared allowance cache is still fresh.
	cache := fmt.Sprintf(`{"ReadAfter":%q,"Sample":{"ObservedAt":%q,"Allowed":true}}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(claudeDirectory, "claude-allowance.json"), []byte(cache), 0600); err != nil {
		t.Fatal(err)
	}
	var claudeReads atomic.Int64
	const modelStartedEvent = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"deployment-response\",\"status\":\"in_progress\"}}\n\n"
	const modelCompletedEvent = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"deployment-response\",\"status\":\"completed\",\"model\":\"gpt-5.6-luna\",\"service_tier\":\"default\",\"usage\":{\"input_tokens\":10,\"input_tokens_details\":{\"cached_tokens\":0},\"output_tokens\":2,\"output_tokens_details\":{\"reasoning_tokens\":0}}}}\n\n"
	modelStarted, modelReleased := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseModel := func() { releaseOnce.Do(func() { close(modelReleased) }) }
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/oauth/usage":
			if request.Header.Get("Authorization") != "Bearer fixture-claude" {
				http.Error(writer, "bad credentials", http.StatusUnauthorized)
				return
			}
			claudeReads.Add(1)
			fmt.Fprintf(writer, `{"five_hour":{"utilization":10,"resets_at":%q}}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case "/backend-api/wham/usage":
			if request.Header.Get("Authorization") != "Bearer "+processAccessToken("deploy-user") || request.Header.Get("ChatGPT-Account-ID") != "subscription-deploy-user" {
				http.Error(writer, "bad credentials", http.StatusUnauthorized)
				return
			}
			io.WriteString(writer, `{"plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false}}`)
		case "/backend-api/codex/models":
			io.WriteString(writer, `{"models":[{"slug":"gpt-5.6-luna"}]}`)
		case "/backend-api/codex/responses":
			writer.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(writer, modelStartedEvent)
			writer.(http.Flusher).Flush()
			close(modelStarted)
			<-modelReleased
			io.WriteString(writer, modelCompletedEvent)
		default:
			io.WriteString(writer, `{}`)
		}
	}))
	defer provider.Close()
	defer releaseModel()
	portReservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := portReservation.Addr().String()
	portReservation.Close()
	environment := append(os.Environ(), "CODEX_SWITCH_DATA_DIR="+data, "CODEX_SWITCH_UPSTREAM_ORIGIN="+provider.URL, "CODEX_SWITCH_CLAUDE_UPSTREAM_ORIGIN="+provider.URL, "CODEX_SWITCH_USAGE_URL="+provider.URL+"/usage", "CODEX_HOME="+root, "CLAUDE_CONFIG_DIR="+root, "PI_CODING_AGENT_SESSION_DIR="+root)
	supervisorContext, stopSupervisor := context.WithCancel(context.Background())
	supervisorStopped := make(chan struct{})
	go func() {
		defer close(supervisorStopped)
		for supervisorContext.Err() == nil {
			generation := exec.CommandContext(supervisorContext, stable, "serve", "--listen", address)
			generation.Env = environment
			_ = generation.Run()
		}
	}()
	t.Cleanup(func() { stopSupervisor(); <-supervisorStopped })
	readStatus := func() deployStatusResponse {
		capability, err := clientcapability.Read(data)
		if err != nil {
			return deployStatusResponse{}
		}
		response, err := (&http.Client{Timeout: time.Second}).Get("http://" + address + clientcapability.PathPrefix(capability) + deployStatusEndpoint)
		if err != nil {
			return deployStatusResponse{}
		}
		defer response.Body.Close()
		var status deployStatusResponse
		_ = json.NewDecoder(response.Body).Decode(&status)
		return status
	}
	deadline := time.Now().Add(10 * time.Second)
	old := readStatus()
	for old.PID == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		old = readStatus()
	}
	if old.PID == 0 {
		t.Fatal("incumbent did not start")
	}
	// Use an unreachable provider only in the candidate, leaving the incumbent
	// connected. The wrapper still runs the actual candidate binary.
	failingCandidate := filepath.Join(root, "unreachable-candidate")
	wrapper := fmt.Sprintf("#!/bin/sh\nexport CODEX_SWITCH_CLAUDE_UPSTREAM_ORIGIN=http://127.0.0.1:1\nexec '%s' \"$@\"\n", binary)
	if err := os.WriteFile(failingCandidate, []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	runUpdate := func(candidate string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "../../deploy/deploy.sh", "new", candidate, "--stable", stable, "--data", data, "--listen", address)
		command.Env = environment
		return command.CombinedOutput()
	}
	output, err := runUpdate(failingCandidate)
	if err == nil || !strings.Contains(string(output), "Claude 1") || !strings.Contains(string(output), "connection refused") || !strings.Contains(string(output), "old generation keeps serving") {
		t.Fatalf("failed update: %v\n%s", err, output)
	}
	if current := readStatus(); current.PID != old.PID {
		t.Fatalf("failed candidate took traffic: incumbent %d, serving %d", old.PID, current.PID)
	}
	stableAfterFailure, err := os.ReadFile(stable)
	if err != nil || string(stableAfterFailure) != string(copyBinary) {
		t.Fatalf("failed candidate replaced stable binary: %v", err)
	}
	logBytes, err := os.ReadFile(proxyLogPath(data))
	if err != nil || !strings.Contains(string(logBytes), "provider check failed: Claude 1") || strings.Contains(string(logBytes), fmt.Sprintf("process %d stopping", old.PID)) {
		t.Fatalf("failed update log: %v\n%s", err, logBytes)
	}
	t.Logf("failed update command:\n%s", output)
	capability, err := clientcapability.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	modelResponse, err := (&http.Client{Timeout: 20 * time.Second}).Post("http://"+address+clientcapability.PathPrefix(capability)+"/backend-api/codex/responses", "application/json", bytes.NewBufferString(`{"model":"gpt-5.6-luna","input":[],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer modelResponse.Body.Close()
	if modelResponse.StatusCode != http.StatusOK {
		t.Fatalf("admitted model stream returned HTTP %d", modelResponse.StatusCode)
	}
	<-modelStarted
	type updateResult struct {
		output []byte
		err    error
	}
	updateFinished := make(chan updateResult, 1)
	go func() { output, err := runUpdate(binary); updateFinished <- updateResult{output, err} }()
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		logBytes, _ = os.ReadFile(proxyLogPath(data))
		if readStatus().PID != old.PID && strings.Contains(string(logBytes), fmt.Sprintf("process %d stopping", old.PID)) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if readStatus().PID == old.PID || !strings.Contains(string(logBytes), fmt.Sprintf("process %d stopping", old.PID)) {
		t.Fatal("new generation did not serve while incumbent held admitted model work")
	}
	select {
	case result := <-updateFinished:
		t.Fatalf("update finished before admitted model work: %v\n%s", result.err, result.output)
	default:
	}
	releaseModel()
	modelBody, err := io.ReadAll(modelResponse.Body)
	if err != nil || string(modelBody) != modelStartedEvent+modelCompletedEvent {
		t.Fatalf("admitted model stream was interrupted: %v, %q", err, modelBody)
	}
	result := <-updateFinished
	output, err = result.output, result.err
	if err != nil {
		t.Fatalf("working update: %v\n%s", err, output)
	}
	if current := readStatus(); current.PID == 0 || current.PID == old.PID || claudeReads.Load() != 1 {
		t.Fatalf("working update: old %d, current %d, fresh Claude reads %d", old.PID, current.PID, claudeReads.Load())
	}
	logBytes, err = os.ReadFile(proxyLogPath(data))
	if err != nil {
		t.Fatal(err)
	}
	passedAt := strings.Index(string(logBytes), "provider check passed")
	stoppedAt := strings.Index(string(logBytes), fmt.Sprintf("process %d stopping", old.PID))
	if passedAt < 0 || stoppedAt <= passedAt {
		t.Fatalf("incumbent stopped before candidate's provider check:\n%s", logBytes)
	}
	t.Logf("working update command:\n%s", output)
	t.Logf("proxy log:\n%s", logBytes)
}
