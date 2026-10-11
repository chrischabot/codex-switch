package codexstate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"

	"github.com/coder/websocket"
)

const appServerMessageLimit = 1 << 20

type rpcClient struct {
	connection *websocket.Conn
	nextID     int64
}

type rpcRequest struct {
	ID     int64  `json:"id,omitempty"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     *int64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("app-server error %d: %s", e.Code, e.Message)
}

type threadReadResult struct {
	Thread appServerThread `json:"thread"`
}

type appServerThread struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	ParentThreadID string                `json:"parentThreadId"`
	AgentNickname  string                `json:"agentNickname"`
	Status         appServerThreadStatus `json:"status"`
}

type appServerThreadStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags"`
}

type configReadResult struct {
	Config map[string]json.RawMessage `json:"config"`
}

type EffectiveModelConfig struct {
	Model         string
	Provider      string
	OpenAIBaseURL *string
}

// ModelConfig reads Codex's effective, layered model configuration through
// the official app-server. An absent provider means the built-in OpenAI
// provider; a nil URL means that provider uses its standard endpoint.
func (r LocalReader) ModelConfig(ctx context.Context) (EffectiveModelConfig, error) {
	value, socketErr := r.modelConfigFromSocket(ctx)
	if socketErr == nil {
		return value, nil
	}
	value, processErr := r.modelConfigFromProcess(ctx)
	if processErr == nil {
		return value, nil
	}
	return EffectiveModelConfig{}, fmt.Errorf("read effective Codex config: existing app-server: %v; official one-shot reader: %w", socketErr, processErr)
}

func (r LocalReader) modelConfigFromSocket(ctx context.Context) (EffectiveModelConfig, error) {
	appContext := ctx
	cancel := func() {}
	if r.AppServerTimeout > 0 {
		appContext, cancel = context.WithTimeout(ctx, r.AppServerTimeout)
	}
	defer cancel()

	connection, err := dialAppServer(appContext, r.AppServerSocket)
	if err != nil {
		return EffectiveModelConfig{}, err
	}
	defer connection.CloseNow()
	connection.SetReadLimit(appServerMessageLimit)

	client := &rpcClient{connection: connection}
	if err := initializeAppServer(appContext, client); err != nil {
		return EffectiveModelConfig{}, err
	}
	var response configReadResult
	if err := client.call(appContext, "config/read", map[string]any{}, &response); err != nil {
		return EffectiveModelConfig{}, err
	}
	return decodeModelConfig(response)
}

// modelConfigFromProcess uses Codex's official config/read implementation when
// ChatGPT.app owns its app-server over private stdio and exposes no attachable
// socket. The child has no listener, performs one read, and is then stopped.
func (r LocalReader) modelConfigFromProcess(ctx context.Context) (EffectiveModelConfig, error) {
	command := r.configCommand
	if command == nil {
		if r.CodexExecutable == "" {
			return EffectiveModelConfig{}, fmt.Errorf("Codex executable is unavailable")
		}
		command = func(ctx context.Context) *exec.Cmd {
			return exec.CommandContext(ctx, r.CodexExecutable, "app-server", "--listen", "stdio://")
		}
	}
	cmd := command(ctx)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return EffectiveModelConfig{}, fmt.Errorf("open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return EffectiveModelConfig{}, fmt.Errorf("open stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return EffectiveModelConfig{}, fmt.Errorf("start Codex app-server: %w", err)
	}
	defer stopProcess(cmd, stdin)

	client := &stdioRPCClient{input: bufio.NewWriter(stdin), output: json.NewDecoder(stdout)}
	if err := initializeStdioAppServer(ctx, client); err != nil {
		return EffectiveModelConfig{}, err
	}
	var response configReadResult
	if err := client.call(ctx, "config/read", map[string]any{}, &response); err != nil {
		return EffectiveModelConfig{}, err
	}
	return decodeModelConfig(response)
}

func decodeModelConfig(response configReadResult) (EffectiveModelConfig, error) {
	if response.Config == nil {
		return EffectiveModelConfig{}, fmt.Errorf("config/read returned no effective config")
	}
	result := EffectiveModelConfig{Provider: "openai"}
	if raw, ok := response.Config["model"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &result.Model); err != nil {
			return EffectiveModelConfig{}, fmt.Errorf("decode effective model: %w", err)
		}
	}
	if raw, ok := response.Config["model_provider"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &result.Provider); err != nil {
			return EffectiveModelConfig{}, fmt.Errorf("decode effective model_provider: %w", err)
		}
		if result.Provider == "" {
			return EffectiveModelConfig{}, fmt.Errorf("effective model_provider is empty")
		}
	}
	raw, ok := response.Config["openai_base_url"]
	if !ok || string(raw) == "null" {
		return result, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return EffectiveModelConfig{}, fmt.Errorf("decode effective openai_base_url: %w", err)
	}
	if value != "" {
		result.OpenAIBaseURL = &value
	}
	return result, nil
}

type stdioRPCClient struct {
	input  *bufio.Writer
	output *json.Decoder
	nextID int64
}

func initializeStdioAppServer(ctx context.Context, client *stdioRPCClient) error {
	if err := client.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "codex-switch", "title": "codex-switch", "version": "0.1.0"},
	}, &struct{}{}); err != nil {
		return err
	}
	return client.notify("initialized")
}

func (c *stdioRPCClient) call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.nextID++
	id := c.nextID
	if err := c.write(rpcRequest{ID: id, Method: method, Params: params}); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var response rpcResponse
		if err := c.output.Decode(&response); err != nil {
			return fmt.Errorf("read %s response: %w", method, err)
		}
		if response.ID == nil || *response.ID != id {
			continue
		}
		if response.Error != nil {
			return response.Error
		}
		if len(response.Result) == 0 {
			return fmt.Errorf("%s returned no result", method)
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
		return nil
	}
}

func (c *stdioRPCClient) notify(method string) error {
	return c.write(rpcRequest{Method: method})
}

func (c *stdioRPCClient) write(value rpcRequest) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := c.input.Write(append(data, '\n')); err != nil {
		return err
	}
	return c.input.Flush()
}

func stopProcess(cmd *exec.Cmd, stdin io.Closer) {
	_ = stdin.Close()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

func (r LocalReader) lookupAppServer(ctx context.Context, requested []string) (map[string]TaskMetadata, bool, error) {
	connection, err := dialAppServer(ctx, r.AppServerSocket)
	if err != nil {
		return nil, false, err
	}
	defer connection.CloseNow()
	connection.SetReadLimit(appServerMessageLimit)

	client := &rpcClient{connection: connection}
	if err := initializeAppServer(ctx, client); err != nil {
		return nil, false, err
	}

	result := make(map[string]TaskMetadata, len(requested))
	queue := append([]string(nil), requested...)
	seen := make(map[string]struct{}, len(requested))
	for len(queue) != 0 {
		id := queue[0]
		queue = queue[1:]
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}

		var response threadReadResult
		if err := client.call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &response); err != nil {
			if isMissingThread(err, id) {
				continue
			}
			return result, true, err
		}
		thread := response.Thread
		if thread.ID != id || canonicalTaskID(thread.ID) != thread.ID {
			return result, true, fmt.Errorf("thread/read returned an unexpected task")
		}
		parentID := thread.ParentThreadID
		relationship := TaskRelationshipKnownRoot
		if parentID != "" && canonicalTaskID(parentID) == parentID {
			relationship = TaskRelationshipKnownParent
		} else if parentID != "" {
			parentID = ""
			relationship = TaskRelationshipUnknown
		}
		result[id] = TaskMetadata{
			TaskID:        id,
			DisplayName:   strings.TrimSpace(thread.Name),
			AgentNickname: strings.TrimSpace(thread.AgentNickname),
			ParentTaskID:  parentID,
			Relationship:  relationship,
			RuntimeStatus: taskRuntimeStatus(thread.Status),
			Sources:       []TaskMetadataSource{TaskMetadataFromAppServer},
		}
		if parentID != "" {
			queue = append(queue, parentID)
		}
	}
	return result, true, nil
}

func initializeAppServer(ctx context.Context, client *rpcClient) error {
	if err := client.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "codex_app_server_daemon",
			"title":   "Codex App Server Daemon",
			"version": "0.1.0",
		},
	}, &struct{}{}); err != nil {
		return err
	}
	if err := client.notify(ctx, "initialized"); err != nil {
		return err
	}
	return nil
}

func taskRuntimeStatus(value appServerThreadStatus) TaskRuntimeStatus {
	result := TaskRuntimeStatus{}
	switch value.Type {
	case string(TaskRuntimeNotLoaded):
		result.State = TaskRuntimeNotLoaded
	case string(TaskRuntimeIdle):
		result.State = TaskRuntimeIdle
	case string(TaskRuntimeSystemError):
		result.State = TaskRuntimeSystemError
	case string(TaskRuntimeActive):
		result.State = TaskRuntimeActive
	default:
		return result
	}
	if result.State != TaskRuntimeActive {
		return result
	}
	for _, flag := range value.ActiveFlags {
		switch flag {
		case string(TaskWaitingOnApproval):
			result.ActiveFlags = append(result.ActiveFlags, TaskWaitingOnApproval)
		case string(TaskWaitingOnUser):
			result.ActiveFlags = append(result.ActiveFlags, TaskWaitingOnUser)
		}
	}
	return result
}

// The installed app-server has no dedicated not-found code. Version
// 0.144.0-alpha.4 reports this exact invalid-request message for a valid UUID
// whose persisted task cannot be loaded. Do not treat other -32600 errors as
// missing history.
func isMissingThread(err error, taskID string) bool {
	var rpcErr *rpcError
	return errors.As(err, &rpcErr) && rpcErr.Code == -32600 && rpcErr.Message == "thread not loaded: "+taskID
}

func dialAppServer(ctx context.Context, socketPath string) (*websocket.Conn, error) {
	transport := &http.Transport{}
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}
	client := &http.Client{Transport: transport}
	connection, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	return connection, nil
}

func (c *rpcClient) call(ctx context.Context, method string, params, result any) error {
	c.nextID++
	id := c.nextID
	if err := c.write(ctx, rpcRequest{ID: id, Method: method, Params: params}); err != nil {
		return err
	}
	for {
		messageType, data, err := c.connection.Read(ctx)
		if err != nil {
			return err
		}
		if messageType != websocket.MessageText {
			continue
		}
		var response rpcResponse
		if err := json.Unmarshal(data, &response); err != nil || response.ID == nil || *response.ID != id {
			continue
		}
		if response.Error != nil {
			return response.Error
		}
		if len(response.Result) == 0 {
			return fmt.Errorf("%s returned no result", method)
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
		return nil
	}
}

func (c *rpcClient) notify(ctx context.Context, method string) error {
	return c.write(ctx, rpcRequest{Method: method})
}

func (c *rpcClient) write(ctx context.Context, value rpcRequest) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.connection.Write(ctx, websocket.MessageText, data)
}
