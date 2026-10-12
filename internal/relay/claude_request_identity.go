package relay

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/joshp123/codex-switch/internal/store"
)

func claudeSessionForRequest(request *http.Request, conversationPrefixes []string) store.TaskID {
	if session := request.Header.Get("X-Claude-Code-Session-Id"); session != "" {
		return store.TaskID("claude:code:" + session)
	}
	if len(conversationPrefixes) > 0 {
		return store.TaskID("claude:conversation:" + conversationPrefixes[0])
	}
	return ""
}

// Only hashes of reusable prompt prefixes survive the request. Cache markers
// move between turns and do not change the content Anthropic caches.
func claudeConversationPrefixes(body []byte) []string {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return nil
	}
	messages, _ := request["messages"].([]any)
	if len(messages) == 0 {
		return nil
	}
	removeCacheMarkers(request["system"])
	if tools, ok := request["tools"].([]any); ok {
		for _, tool := range tools {
			if fields, ok := tool.(map[string]any); ok {
				delete(fields, "cache_control")
			}
		}
	}
	prompt := map[string]any{}
	for _, key := range []string{"model", "system", "tools", "thinking", "tool_choice"} {
		if value, exists := request[key]; exists {
			prompt[key] = value
		}
	}
	initial, err := json.Marshal(prompt)
	if err != nil {
		return nil
	}
	fingerprint := sha256.New()
	fingerprint.Write(initial)
	var prefixes []string
	for _, message := range messages {
		fields, _ := message.(map[string]any)
		removeCacheMarkers(fields["content"])
		encoded, err := json.Marshal(message)
		if err != nil {
			return nil
		}
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(encoded)))
		fingerprint.Write(size[:])
		fingerprint.Write(encoded)
		if fields["role"] == "user" {
			prefixes = append(prefixes, hex.EncodeToString(fingerprint.Sum(nil)))
		}
	}
	return prefixes
}

func removeCacheMarkers(content any) {
	blocks, _ := content.([]any)
	for _, block := range blocks {
		if fields, ok := block.(map[string]any); ok {
			delete(fields, "cache_control")
			if fields["type"] == "tool_result" {
				removeCacheMarkers(fields["content"])
			}
		}
	}
}
