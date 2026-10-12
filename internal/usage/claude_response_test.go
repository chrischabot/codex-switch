package usage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/joshp123/codex-switch/internal/store"
)

// The Messages API sends inclusive cumulative deltas, not increments, and
// only message_stop proves the streamed response completed.
func TestClaudeStreamUsageAndMixedCachePrices(t *testing.T) {
	observation := NewClaudeResponseUsage("claude-opus-5-5", "standard", "global")
	events := []string{
		`{"type":"message_start","message":{"id":"msg_example","model":"claude-opus-5-5","usage":{"input_tokens":100,"output_tokens":1,"cache_read_input_tokens":1000,"cache_creation_input_tokens":400,"cache_creation":{"ephemeral_5m_input_tokens":300,"ephemeral_1h_input_tokens":100},"speed":"fast"}}}`,
		`{"type":"message_delta","usage":{"output_tokens":10}}`,
		`{"type":"message_delta","usage":{"output_tokens":20}}`,
	}
	for _, event := range events {
		if completed, err := observation.Observe([]byte(event)); completed || err != nil {
			t.Fatalf("unfinished stream: completed=%t, error=%v", completed, err)
		}
	}
	if _, err := observation.Record("session", "account", time.Now()); err == nil {
		t.Fatal("recorded an unfinished stream")
	}
	if completed, err := observation.Observe([]byte(`{"type":"message_stop"}`)); !completed || err != nil {
		t.Fatalf("terminal event: completed=%t, error=%v", completed, err)
	}
	record, err := observation.Record("session", "account", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if record.InputTokens != 1500 || record.CachedInputTokens != 1000 || *record.CacheWriteInputTokens != 400 || record.OutputTokens != 20 {
		t.Fatalf("normalised token counts: %+v", record)
	}
	// Published Opus 5.5 Fast rates: $8 input, $0.40 cache read,
	// $10 five-minute write, $16 one-hour write, $40 output per million.
	if record.CostNanoUSD == nil || *record.CostNanoUSD != 6600000 {
		t.Fatalf("price=%v, want 6600000 nano-USD", record.CostNanoUSD)
	}
	database, err := store.OpenStoreDatabase(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.CloseStoreDatabase()
	for range 2 {
		if err := database.RecordCompletedResponse(record); err != nil {
			t.Fatal(err)
		}
	}
	responses, err := database.ReadCompletedResponsesSince(record.CompletedAt.Add(-time.Second))
	if err != nil || len(responses) != 1 {
		t.Fatalf("deduplicated ledger responses=%+v, error=%v", responses, err)
	}
	completions, err := reconcileCompletions(nil, responses)
	if err != nil || len(completions) != 1 || completions[0].Cost == nil || *completions[0].Cost != 6600000 {
		t.Fatalf("shared ledger price: completions=%+v, error=%v", completions, err)
	}
}

func TestClaudeHaikuContextTierIncludesCachedTokens(t *testing.T) {
	observation := NewClaudeResponseUsage("claude-haiku-5-5-20261001", "", "us")
	complete, err := observation.Observe([]byte(`{"type":"message","id":"msg_example","model":"claude-haiku-5-5-20261001","usage":{"input_tokens":1,"cache_read_input_tokens":100000,"output_tokens":10}}`))
	if !complete || err != nil {
		t.Fatalf("JSON message: complete=%t, error=%v", complete, err)
	}
	record, err := observation.Record("session", "account", time.Now())
	// Above 100k input, Haiku costs $0.50 input, $0.05 read, $2.50
	// output per million. US inference multiplies token prices by 1.1.
	if err != nil || record.CostNanoUSD == nil || *record.CostNanoUSD != 5528050 {
		t.Fatalf("price=%v, error=%v, want 5528050 nano-USD", record.CostNanoUSD, err)
	}
}
