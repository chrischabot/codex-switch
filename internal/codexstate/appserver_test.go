package codexstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const fixtureParent = "018f0000-0000-7000-8000-000000000012"
const fixtureChild = "018f0000-0000-7000-8000-000000000015"

type appServerFixture struct {
	t           *testing.T
	listener    net.Listener
	server      *http.Server
	socketPath  string
	threads     map[string]any
	config      map[string]any
	closeOnRead map[string]bool

	mu       sync.Mutex
	requests []fixtureRequest
}

type fixtureRequest struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type threadReadParams struct {
	ThreadID     string `json:"threadId"`
	IncludeTurns bool   `json:"includeTurns"`
}

func TestLookupUsesOnlyReadOnlyAppServerMetadata(t *testing.T) {
	server := startAppServerFixture(t, map[string]any{
		fixtureChild: map[string]any{
			"id":             fixtureChild,
			"name":           nil,
			"parentThreadId": fixtureParent,
			"agentNickname":  "Pascal",
			"status": map[string]any{
				"type":        "active",
				"activeFlags": []string{"waitingOnApproval"},
			},
			"source": "vscode",
			"turns":  []any{},
		},
		fixtureParent: map[string]any{
			"id":             fixtureParent,
			"name":           " Parent task ",
			"parentThreadId": nil,
			"agentNickname":  nil,
			"status":         map[string]any{"type": "idle"},
			"source":         "vscode",
			"turns":          []any{},
		},
	})
	defer server.close()

	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	got, err := reader.Lookup(t.Context(), []string{fixtureChild, fixtureChild, "prefix-" + fixtureChild})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("metadata = %#v", got)
	}
	child := got[fixtureChild]
	if child.DisplayName != "" || child.AgentNickname != "Pascal" || child.ParentTaskID != fixtureParent ||
		child.Relationship != TaskRelationshipKnownParent ||
		child.RuntimeStatus.State != "active" || len(child.RuntimeStatus.ActiveFlags) != 1 || child.RuntimeStatus.ActiveFlags[0] != "waitingOnApproval" {
		t.Fatalf("child = %#v", child)
	}
	if len(child.Sources) != 1 || child.Sources[0] != TaskMetadataFromAppServer {
		t.Fatalf("app-server source = %#v", child.Sources)
	}
	parent := got[fixtureParent]
	if parent.DisplayName != "Parent task" || parent.AgentNickname != "" || parent.ParentTaskID != "" ||
		parent.Relationship != TaskRelationshipKnownRoot || parent.RuntimeStatus.State != "idle" {
		t.Fatalf("parent = %#v", parent)
	}
	server.assertReadOnlyRequests(t, []string{fixtureChild, fixtureParent})
}

func TestLookupPrefersCanonicalTopLevelRelationshipFields(t *testing.T) {
	otherParent := "018f0000-0000-7000-8000-000000000002"
	server := startAppServerFixture(t, map[string]any{
		fixtureChild: map[string]any{
			"id":             fixtureChild,
			"parentThreadId": fixtureParent,
			"agentNickname":  "Kepler",
			"source": map[string]any{"subAgent": map[string]any{"thread_spawn": map[string]any{
				"parent_thread_id": otherParent,
				"agent_nickname":   "Pascal",
			}}},
		},
		fixtureParent: map[string]any{"id": fixtureParent, "source": "vscode"},
	})
	defer server.close()
	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	got, err := reader.Lookup(t.Context(), []string{fixtureChild})
	if err != nil {
		t.Fatal(err)
	}
	if got[fixtureChild].ParentTaskID != fixtureParent || got[fixtureChild].AgentNickname != "Kepler" ||
		got[fixtureChild].Relationship != TaskRelationshipKnownParent {
		t.Fatalf("child = %#v", got[fixtureChild])
	}
}

func TestLookupDoesNotGuessRootFromInvalidAppServerParent(t *testing.T) {
	server := startAppServerFixture(t, map[string]any{
		fixtureChild: map[string]any{"id": fixtureChild, "parentThreadId": "not-a-task-id"},
	})
	defer server.close()
	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}

	got, err := reader.Lookup(t.Context(), []string{fixtureChild})
	if err != nil {
		t.Fatal(err)
	}
	if metadata := got[fixtureChild]; metadata.ParentTaskID != "" || metadata.Relationship != TaskRelationshipUnknown {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestLookupReturnsTaskNameFromAppServer(t *testing.T) {
	server := startAppServerFixture(t, map[string]any{
		fixtureParent: map[string]any{"id": fixtureParent, "name": "Parent task", "source": "vscode"},
	})
	defer server.close()
	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	got, err := reader.Lookup(t.Context(), []string{fixtureParent})
	if err != nil {
		t.Fatal(err)
	}
	if got[fixtureParent].DisplayName != "Parent task" {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestLookupKeepsAvailableMetadataWhenAnOldTaskIsMissing(t *testing.T) {
	server := startAppServerFixture(t, map[string]any{
		fixtureParent: map[string]any{"id": fixtureParent, "name": "Parent task", "source": "vscode"},
	})
	defer server.close()
	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	got, err := reader.Lookup(t.Context(), []string{fixtureChild, fixtureParent})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "1 task detail record remains unresolved") {
		t.Fatalf("error = %v", err)
	}
	if len(got) != 1 || got[fixtureParent].DisplayName != "Parent task" {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestLookupFillsMissingLiveAppServerTaskFromLocalRecords(t *testing.T) {
	home := t.TempDir()
	writeLocalNames(t, home, fmt.Sprintf(`{"id":%q,"thread_name":"Local child"}`, fixtureChild))
	writeLocalRollout(t, home, fixtureChild, fmt.Sprintf(
		`{"timestamp":"2026-07-14T10:01:00Z","type":"session_meta","payload":{"id":%q,"source":"vscode"}}`, fixtureChild,
	))
	server := startAppServerFixture(t, map[string]any{})
	defer server.close()
	reader := LocalReader{AppServerSocket: server.listener.Addr().String(), CodexHome: home}

	got, err := reader.Lookup(t.Context(), []string{fixtureChild})
	if err != nil {
		t.Fatal(err)
	}
	if child := got[fixtureChild]; child.DisplayName != "Local child" ||
		child.Relationship != TaskRelationshipKnownRoot ||
		len(child.Sources) != 1 || child.Sources[0] != TaskMetadataFromLocalFiles {
		t.Fatalf("metadata = %#v, error = %v", got, err)
	}
	server.assertReadOnlyRequests(t, []string{fixtureChild})
}

func TestLookupFillsAppServerGapsWithoutOverridingItsFields(t *testing.T) {
	home := t.TempDir()
	writeLocalNames(t, home, fmt.Sprintf(`{"id":%q,"thread_name":"Local child"}`, fixtureChild))
	writeLocalRollout(t, home, fixtureChild, fmt.Sprintf(
		`{"timestamp":"2026-07-14T10:01:00Z","type":"session_meta","payload":{"id":%q,"agent_nickname":"Local nickname","source":{"subagent":{"thread_spawn":{"parent_thread_id":%q}}}}}`, fixtureChild, fixtureParent,
	))
	server := startAppServerFixture(t, map[string]any{
		fixtureChild: map[string]any{
			"id": fixtureChild, "name": nil, "parentThreadId": fixtureParent,
			"agentNickname": "App-server nickname", "status": map[string]any{"type": "active"},
		},
		fixtureParent: map[string]any{
			"id": fixtureParent, "name": "App-server parent", "parentThreadId": nil,
			"status": map[string]any{"type": "idle"},
		},
	})
	defer server.close()
	reader := LocalReader{AppServerSocket: server.listener.Addr().String(), CodexHome: home}

	got, err := reader.Lookup(t.Context(), []string{fixtureChild})
	if err != nil {
		t.Fatal(err)
	}
	child := got[fixtureChild]
	if child.DisplayName != "Local child" || child.AgentNickname != "App-server nickname" ||
		child.ParentTaskID != fixtureParent || child.Relationship != TaskRelationshipKnownParent ||
		child.RuntimeStatus.State != TaskRuntimeActive {
		t.Fatalf("child = %#v", child)
	}
	wantSources := []TaskMetadataSource{
		TaskMetadataFromAppServer,
		TaskMetadataFromLocalFiles,
	}
	if len(child.Sources) != len(wantSources) || child.Sources[0] != wantSources[0] || child.Sources[1] != wantSources[1] {
		t.Fatalf("child sources = %#v, want %#v", child.Sources, wantSources)
	}
	if parent := got[fixtureParent]; parent.DisplayName != "App-server parent" ||
		parent.Relationship != TaskRelationshipKnownRoot || parent.RuntimeStatus.State != TaskRuntimeIdle {
		t.Fatalf("parent = %#v", parent)
	}
}

func TestLookupDoesNotRetainAStaleLocalParentForAnAppServerRoot(t *testing.T) {
	home := t.TempDir()
	writeLocalNames(t, home, fmt.Sprintf(`{"id":%q,"thread_name":"Local child"}`, fixtureChild))
	writeLocalRollout(t, home, fixtureChild, fmt.Sprintf(
		`{"timestamp":"2026-07-14T10:01:00Z","type":"session_meta","payload":{"id":%q,"source":{"subagent":{"thread_spawn":{"parent_thread_id":%q}}}}}`,
		fixtureChild, fixtureParent,
	))
	server := startAppServerFixture(t, map[string]any{
		fixtureChild: map[string]any{
			"id": fixtureChild, "name": nil, "parentThreadId": nil,
			"status": map[string]any{"type": "idle"},
		},
	})
	defer server.close()

	got, err := (LocalReader{AppServerSocket: server.listener.Addr().String(), CodexHome: home}).Lookup(
		t.Context(), []string{fixtureChild},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := got[fixtureParent]; exists {
		t.Fatalf("stale local parent leaked into app-server result: %#v", got)
	}
	if child := got[fixtureChild]; child.DisplayName != "Local child" ||
		child.Relationship != TaskRelationshipKnownRoot {
		t.Fatalf("child = %#v", child)
	}
}

func TestLookupDoesNotReadFallbackWhenAppServerResolvesEveryTask(t *testing.T) {
	server := startAppServerFixture(t, map[string]any{
		fixtureParent: map[string]any{"id": fixtureParent, "name": "Parent task", "status": map[string]any{"type": "idle"}},
	})
	defer server.close()
	reader := LocalReader{
		AppServerSocket: server.listener.Addr().String(),
		CodexHome:       filepath.Join(t.TempDir(), "not-a-directory"),
	}
	if err := os.WriteFile(reader.CodexHome, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := reader.Lookup(t.Context(), []string{fixtureParent})
	if err != nil || got[fixtureParent].DisplayName != "Parent task" {
		t.Fatalf("metadata = %#v, error = %v", got, err)
	}
}

func TestLookupDoesNotHideConnectionFailureAfterOneSuccessfulRead(t *testing.T) {
	server := startAppServerFixture(t, map[string]any{
		fixtureParent: map[string]any{"id": fixtureParent, "name": "Parent task", "source": "vscode"},
	})
	server.closeOnRead = map[string]bool{fixtureChild: true}
	defer server.close()
	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	got, err := reader.Lookup(t.Context(), []string{fixtureParent, fixtureChild})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if len(got) != 1 || got[fixtureParent].DisplayName != "Parent task" {
		t.Fatalf("partial metadata = %#v", got)
	}
}

func TestLookupRequiresExistingAppServer(t *testing.T) {
	reader := LocalReader{AppServerSocket: filepath.Join(t.TempDir(), "missing.sock")}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := reader.Lookup(ctx, []string{fixtureParent}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestLookupReturnsEmptyWithoutConnectingForInvalidIDs(t *testing.T) {
	reader := LocalReader{AppServerSocket: filepath.Join(t.TempDir(), "missing.sock")}
	got, err := reader.Lookup(t.Context(), []string{"not-a-task", strings.ToUpper(fixtureParent)})
	if err != nil || len(got) != 0 {
		t.Fatalf("metadata = %#v, error = %v", got, err)
	}
}

func TestLookupRejectsUnexpectedThreadID(t *testing.T) {
	server := startAppServerFixture(t, map[string]any{
		fixtureParent: map[string]any{"id": fixtureChild, "source": "vscode"},
	})
	defer server.close()
	reader := LocalReader{AppServerSocket: server.listener.Addr().String()}
	if _, err := reader.Lookup(t.Context(), []string{fixtureParent}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func startAppServerFixture(t *testing.T, threads map[string]any) *appServerFixture {
	t.Helper()
	placeholder, err := os.CreateTemp("/tmp", "codex-switch-*.sock")
	if err != nil {
		t.Fatal(err)
	}
	path := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &appServerFixture{t: t, listener: listener, socketPath: path, threads: threads}
	fixture.server = &http.Server{Handler: http.HandlerFunc(fixture.serve)}
	go func() {
		if err := fixture.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("app-server fixture: %v", err)
		}
	}()
	return fixture
}

func (f *appServerFixture) serve(output http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		f.t.Errorf("WebSocket path = %q", request.URL.Path)
	}
	connection, err := websocket.Accept(output, request, nil)
	if err != nil {
		f.t.Errorf("accept WebSocket: %v", err)
		return
	}
	defer connection.CloseNow()
	for {
		messageType, data, err := connection.Read(request.Context())
		if err != nil {
			return
		}
		if messageType != websocket.MessageText {
			continue
		}
		var call fixtureRequest
		if err := json.Unmarshal(data, &call); err != nil {
			f.t.Errorf("decode request: %v", err)
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, call)
		f.mu.Unlock()
		switch call.Method {
		case "initialize":
			var params struct {
				ClientInfo struct {
					Name string `json:"name"`
				} `json:"clientInfo"`
			}
			if err := json.Unmarshal(call.Params, &params); err != nil || params.ClientInfo.Name != "codex_app_server_daemon" {
				f.t.Errorf("initialize params = %s", call.Params)
				return
			}
			f.reply(connection, request.Context(), call.ID, map[string]any{"userAgent": "fixture"})
		case "initialized":
			if call.ID != 0 {
				f.t.Errorf("initialized carried id %d", call.ID)
			}
		case "thread/read":
			var params threadReadParams
			if err := json.Unmarshal(call.Params, &params); err != nil || params.IncludeTurns {
				f.t.Errorf("thread/read params = %s", call.Params)
				return
			}
			if f.closeOnRead[params.ThreadID] {
				connection.CloseNow()
				return
			}
			thread, ok := f.threads[params.ThreadID]
			if !ok {
				f.replyError(connection, request.Context(), call.ID, -32600, "thread not loaded: "+params.ThreadID)
				continue
			}
			f.reply(connection, request.Context(), call.ID, map[string]any{"thread": thread})
		case "config/read":
			var params map[string]any
			if err := json.Unmarshal(call.Params, &params); err != nil || len(params) != 0 {
				f.t.Errorf("config/read params = %s", call.Params)
				return
			}
			f.reply(connection, request.Context(), call.ID, map[string]any{
				"config":  f.config,
				"origins": map[string]any{},
				"layers":  nil,
			})
		default:
			f.t.Errorf("unexpected method %q", call.Method)
			return
		}
	}
}

func (f *appServerFixture) reply(connection *websocket.Conn, ctx context.Context, id int64, result any) {
	f.write(connection, ctx, map[string]any{"id": id, "result": result})
}

func (f *appServerFixture) replyError(connection *websocket.Conn, ctx context.Context, id int64, code int, message string) {
	f.write(connection, ctx, map[string]any{"id": id, "error": map[string]any{"code": code, "message": message}})
}

func (f *appServerFixture) write(connection *websocket.Conn, ctx context.Context, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		f.t.Errorf("encode response: %v", err)
		return
	}
	if err := connection.Write(ctx, websocket.MessageText, data); err != nil {
		f.t.Errorf("write response: %v", err)
	}
}

func (f *appServerFixture) assertReadOnlyRequests(t *testing.T, ids []string) {
	t.Helper()
	f.mu.Lock()
	requests := append([]fixtureRequest(nil), f.requests...)
	f.mu.Unlock()
	if len(requests) != 2+len(ids) || requests[0].Method != "initialize" || requests[1].Method != "initialized" {
		t.Fatalf("requests = %#v", requests)
	}
	for index, id := range ids {
		request := requests[index+2]
		var params threadReadParams
		if request.Method != "thread/read" || json.Unmarshal(request.Params, &params) != nil || params.ThreadID != id || params.IncludeTurns {
			t.Fatalf("request %d = %#v", index+2, request)
		}
	}
}

func (f *appServerFixture) close() {
	_ = f.server.Close()
	_ = f.listener.Close()
	_ = os.Remove(f.socketPath)
}
