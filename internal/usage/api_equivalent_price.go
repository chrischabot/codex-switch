package usage

import "strings"

// nanoUSD stores one billionth of a US dollar. Published API token rates are
// exact in this unit.
type nanoUSD int64

// harnessMode is the Codex speed setting a response ran in. Fast responses are
// priced at the model's Fast multiplier.
type harnessMode string

const (
	harnessStandard harnessMode = "standard"
	harnessFast     harnessMode = "fast"
)

type tokenRates struct {
	input      nanoUSD
	cached     nanoUSD
	cacheWrite nanoUSD
	output     nanoUSD
}

type multiplier struct {
	numerator   int64
	denominator int64
}

var (
	fast2x   = multiplier{numerator: 2, denominator: 1}
	fast2p5x = multiplier{numerator: 5, denominator: 2}
	standard = multiplier{numerator: 1, denominator: 1}
)

type modelRate struct {
	requiresCacheWrites     bool
	supportsLongContextFast bool
	longContextThreshold    int64
	short                   tokenRates
	long                    tokenRates
	fast                    multiplier
}

// apiEquivalentPrice prices one response at OpenAI's published API rates. It
// returns nil when the model, harness mode or token counts have no exact price.
func apiEquivalentPrice(model string, mode harnessMode, usage tokenUsage) *nanoUSD {
	if !validTokenUsage(usage) {
		return nil
	}
	rate, found := openAIAPIServiceTierRates[strings.TrimSpace(model)]
	if !found || (rate.requiresCacheWrites && usage.CacheWriteInputTokens == nil) {
		return nil
	}
	rates := rate.short
	longContext := usage.InputTokens > rate.longContextThreshold
	if longContext {
		rates = rate.long
	}
	scale := standard
	switch mode {
	case harnessStandard:
	case harnessFast:
		if longContext && !rate.supportsLongContextFast {
			return nil
		}
		scale = rate.fast
	default:
		return nil
	}
	uncached := usage.InputTokens - usage.CachedInputTokens
	cacheWrite := int64(0)
	if rate.requiresCacheWrites {
		cacheWrite = *usage.CacheWriteInputTokens
		uncached -= cacheWrite
	}
	cost := nanoUSD(uncached)*rates.input + nanoUSD(usage.CachedInputTokens)*rates.cached +
		nanoUSD(cacheWrite)*rates.cacheWrite + nanoUSD(usage.OutputTokens)*rates.output
	cost = scale.apply(cost)
	return &cost
}

// apply rounds half a nano-US dollar up.
func (scale multiplier) apply(cost nanoUSD) nanoUSD {
	scaled := int64(cost) * scale.numerator
	whole, remainder := scaled/scale.denominator, scaled%scale.denominator
	if remainder != 0 && remainder >= (scale.denominator+1)/2 {
		whole++
	}
	return nanoUSD(whole)
}

func validTokenUsage(usage tokenUsage) bool {
	if usage.InputTokens < 0 || usage.CachedInputTokens < 0 || usage.OutputTokens < 0 || usage.ReasoningOutputTokens < 0 {
		return false
	}
	if usage.CachedInputTokens > usage.InputTokens || usage.ReasoningOutputTokens > usage.OutputTokens {
		return false
	}
	cacheWrite := usage.CacheWriteInputTokens
	return cacheWrite == nil || (*cacheWrite >= 0 && *cacheWrite <= usage.InputTokens-usage.CachedInputTokens)
}
