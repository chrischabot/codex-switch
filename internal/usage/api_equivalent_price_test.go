package usage

import "testing"

// Expected costs come from the GPT-6 Astra credit rates published at
// https://learn.chatgpt.com/docs/pricing (read 11 October 2026): 250 credits
// per million input tokens, 25 cached and 1,250 output, at 25 credits per US
// dollar. Those equal the API's $10, $1 and $50 per million.
func TestAstraMatchesPublishedRates(t *testing.T) {
	usage := tokenUsage{InputTokens: 400_000, CachedInputTokens: 300_000, OutputTokens: 10_000}
	standard := nanoUSD(100_000*10_000 + 300_000*1_000 + 10_000*50_000)
	for mode, want := range map[harnessMode]nanoUSD{harnessStandard: standard, harnessFast: 2 * standard} {
		if cost := apiEquivalentPrice("gpt-6-astra", mode, usage); cost == nil || *cost != want {
			t.Errorf("%s price = %v, want %d nano-USD", mode, cost, want)
		}
	}
}
