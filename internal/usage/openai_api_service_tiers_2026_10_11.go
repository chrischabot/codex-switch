package usage

import "math"

// Current API-equivalent value, using published rates read 11 October 2026.
// This is not a reconstruction of historical invoices or ChatGPT credit charges.
// Standard, Fast and long-context (>272K input tokens) rates for every model:
// https://developers.openai.com/api/docs/pricing
// Per-model rules and snapshot IDs: https://developers.openai.com/api/docs/models/<model>
// Codex sends service_tier "priority", which is billed as Fast mode:
// https://developers.openai.com/api/docs/guides/fast-mode
// GPT-5.6 Sol's promotional rates hold at least through 21 November 2026:
// https://developers.openai.com/api/docs/changelog
const longContextThreshold = int64(272_000)

// openAIAPIServiceTierRates contains only explicit OpenAI model IDs, aliases, snapshots,
// token rates, long-context rules and API service-tier pricing.
var openAIAPIServiceTierRates = map[string]modelRate{
	"gpt-6.1-sol": longContextModel(
		rates(2_000, 100, 2_500, 10_000),
		rates(4_000, 200, 5_000, 15_000),
		true,
		fast2x,
		true,
	),
	"gpt-6-sol": longContextModel(
		rates(2_000, 200, 2_500, 10_000),
		rates(4_000, 400, 5_000, 15_000),
		true,
		fast2x,
		true,
	),
	"gpt-6-luna": longContextModel(
		rates(100, 10, 125, 500),
		rates(200, 20, 250, 750),
		true,
		fast2x,
		true,
	),
	"gpt-6-astra": longContextModel(
		rates(10_000, 1_000, 12_500, 50_000),
		rates(20_000, 2_000, 25_000, 75_000),
		true,
		fast2x,
		true,
	),
	"gpt-5.6-sol": longContextModel(
		rates(4_000, 400, 5_000, 20_000),
		rates(8_000, 800, 10_000, 30_000),
		true,
		fast2x,
		true,
	),
	// gpt-5.6 is an alias of gpt-5.6-sol.
	"gpt-5.6": longContextModel(
		rates(4_000, 400, 5_000, 20_000),
		rates(8_000, 800, 10_000, 30_000),
		true,
		fast2x,
		true,
	),
	"gpt-5.6-terra": longContextModel(
		rates(2_000, 200, 2_500, 12_000),
		rates(4_000, 400, 5_000, 18_000),
		true,
		fast2x,
		true,
	),
	"gpt-5.6-luna": longContextModel(
		rates(200, 20, 250, 1_200),
		rates(400, 40, 500, 1_800),
		true,
		fast2x,
		true,
	),
	"gpt-5.5": longContextModel(
		rates(5_000, 500, 5_000, 30_000),
		rates(10_000, 1_000, 10_000, 45_000),
		false,
		fast2p5x,
		false,
	),
	"gpt-5.5-2026-04-23": longContextModel(
		rates(5_000, 500, 5_000, 30_000),
		rates(10_000, 1_000, 10_000, 45_000),
		false,
		fast2p5x,
		false,
	),
	"gpt-5.4": longContextModel(
		rates(2_500, 250, 2_500, 15_000),
		rates(5_000, 500, 5_000, 22_500),
		false,
		fast2x,
		false,
	),
	"gpt-5.4-2026-03-05": longContextModel(
		rates(2_500, 250, 2_500, 15_000),
		rates(5_000, 500, 5_000, 22_500),
		false,
		fast2x,
		false,
	),
	"gpt-5.4-mini": singleRateModel(
		rates(750, 75, 750, 4_500), false, fast2x,
	),
	"gpt-5.4-mini-2026-03-17": singleRateModel(
		rates(750, 75, 750, 4_500), false, fast2x,
	),
	"gpt-5.3-codex": singleRateModel(
		rates(1_750, 175, 1_750, 14_000), false, fast2x,
	),
}

func singleRateModel(
	rate tokenRates,
	requiresCacheWrites bool,
	fast multiplier,
) modelRate {
	return modelRate{
		requiresCacheWrites:  requiresCacheWrites,
		longContextThreshold: math.MaxInt64, short: rate, long: rate, fast: fast,
	}
}

func longContextModel(
	short tokenRates,
	long tokenRates,
	requiresCacheWrites bool,
	fast multiplier,
	supportsLongContextFast bool,
) modelRate {
	return modelRate{
		requiresCacheWrites: requiresCacheWrites, supportsLongContextFast: supportsLongContextFast,
		longContextThreshold: longContextThreshold, short: short, long: long, fast: fast,
	}
}

// Rates are nano-US dollars per token. For example, $5 per million tokens is
// exactly 5,000 nano-US dollars per token.
func rates(input, cached, cacheWrite, output int64) tokenRates {
	return tokenRates{
		input: nanoUSD(input), cached: nanoUSD(cached),
		cacheWrite: nanoUSD(cacheWrite), output: nanoUSD(output),
	}
}
