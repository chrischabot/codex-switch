package codexstate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

type EffectiveModelConfig struct {
	Model         string
	Provider      string
	OpenAIBaseURL *string
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

type configReadResult struct {
	Config map[string]json.RawMessage `json:"config"`
}

// ModelConfig reads Codex's effective, layered model configuration with the
// official config/read call. ChatGPT.app owns its app-server over private
// stdio, so this starts a one-shot app-server with no listener, performs one
// read and stops it. An absent provider means the built-in OpenAI provider; a
// nil URL means that provider uses its standard endpoint.
func (r LocalReader) ModelConfig(ctx context.Context) (EffectiveModelConfig, error) {
	value, err := r.readModelConfig(ctx)
	if err != nil {
		return EffectiveModelConfig{}, fmt.Errorf("read effective Codex config: %w", err)
	}
	return value, nil
}

func (r LocalReader) readModelConfig(ctx context.Context) (EffectiveModelConfig, error) {
	if r.CodexExecutable == "" {
		return EffectiveModelConfig{}, fmt.Errorf("Codex executable is unavailable")
	}
	cmd := exec.CommandContext(ctx, r.CodexExecutable, "app-server", "--listen", "stdio://")
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
	if err := client.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "codex-switch", "title": "codex-switch", "version": "0.1.0"},
	}, &struct{}{}); err != nil {
		return EffectiveModelConfig{}, err
	}
	if err := client.write(rpcRequest{Method: "initialized"}); err != nil {
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
