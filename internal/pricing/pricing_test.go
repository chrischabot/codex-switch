package pricing

import (
	"testing"
)

// Expected costs come from the per-1M-token GPT-6.1 Sol rates published at
// https://developers.openai.com/api/docs/pricing (read 11 October 2026).
func TestGPT61SolMatchesPublishedPricing(t *testing.T) {
	catalog := OpenAIAPIServiceTiers()
	for _, test := range []struct {
		name   string
		mode   HarnessMode
		tokens TokenUsage
		want   NanoUSD
	}{
		{
			// Standard, short context: $2 input, $0.10 cached, $2.50 cache writes, $10 output.
			name: "standard short context", mode: HarnessStandard,
			tokens: usage(200_000, 100_000, 50_000, 10_000),
			want:   50_000*2_000 + 100_000*100 + 50_000*2_500 + 10_000*10_000,
		},
		{
			// Fast, long context: $8 input, $0.40 cached, $10 cache writes, $30 output.
			name: "fast long context", mode: HarnessFast,
			tokens: usage(300_000, 100_000, 100_000, 10_000),
			want:   100_000*8_000 + 100_000*400 + 100_000*10_000 + 10_000*30_000,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := catalog.PriceItem(Item{
				Model: "gpt-6.1-sol", Tokens: test.tokens, CacheWriteKnown: true, HarnessMode: test.mode,
			})
			if !result.Known() || *result.Cost != test.want {
				t.Fatalf("price = %#v, want %d nano-USD", result, test.want)
			}
		})
	}
}

func usage(input, cached, cacheWrite, output int64) TokenUsage {
	return TokenUsage{
		InputTokens: input, CachedInputTokens: cached, CacheWriteTokens: cacheWrite,
		OutputTokens: output, TotalTokens: input + output,
	}
}
