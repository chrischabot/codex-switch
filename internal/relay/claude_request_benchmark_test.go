package relay

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Long text contexts include moving cache markers.
// Forwarded-byte and fork-affinity tests remain the correctness contract.
func BenchmarkClaudeConversationPrefixes(b *testing.B) {
	for _, size := range []int{1 << 20, 8 << 20} {
		b.Run(fmt.Sprintf("%dMiB", size>>20), func(b *testing.B) {
			messages := make([]any, 64)
			for i := range messages {
				role := "assistant"
				if i%2 == 0 {
					role = "user"
				}
				messages[i] = map[string]any{"role": role, "content": []any{map[string]any{"type": "text", "text": strings.Repeat("context ", size/64/8), "cache_control": map[string]any{"type": "ephemeral"}}}}
			}
			body, err := json.Marshal(map[string]any{"model": "claude-sonnet-4-6", "system": "fixture", "messages": messages, "max_tokens": 1024})
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if len(claudeConversationPrefixes(body)) != 32 {
					b.Fatal("lost prefixes")
				}
			}
		})
	}
}
