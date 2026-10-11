package pricing

import (
	"math"
	"strings"
)

// TokenUsage is the measured token classes of one model response.
type TokenUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	CacheWriteTokens  int64 `json:"cache_write_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	ReasoningTokens   int64 `json:"reasoning_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
}

type Version string

type Basis string

// OpenAIAPIServiceTierTextTokens prices measured token classes at OpenAI's
// published API service-tier rates.
const OpenAIAPIServiceTierTextTokens Basis = "openai-api-service-tier-text-token-cost"

type HarnessMode string

const (
	HarnessStandard HarnessMode = "standard"
	HarnessFast     HarnessMode = "fast"
)

// NanoUSD stores one billionth of a US dollar. Published API token rates are
// exact in this unit.
type NanoUSD int64

type ContextClass string

const (
	ShortContext ContextClass = "short"
	LongContext  ContextClass = "long"
)

type UnknownReason string

const (
	UnknownModel              UnknownReason = "model-price-not-in-rate-card"
	UnknownInvalidTokens      UnknownReason = "invalid-token-usage"
	UnknownCacheWriteRequired UnknownReason = "cache-write-token-count-required"
	UnknownCostOverflow       UnknownReason = "cost-overflow"
	UnknownHarnessMode        UnknownReason = "harness-mode-not-recognised"
	UnknownFastUnsupported    UnknownReason = "fast-mode-not-supported-for-model"
)

type Result struct {
	Model          string        `json:"model"`
	CanonicalModel string        `json:"canonical_model,omitempty"`
	RateVersion    Version       `json:"rate_version"`
	Basis          Basis         `json:"basis"`
	Context        ContextClass  `json:"context,omitempty"`
	HarnessMode    HarnessMode   `json:"harness_mode"`
	Cost           *NanoUSD      `json:"nano_usd,omitempty"`
	UnknownReason  UnknownReason `json:"unknown_reason,omitempty"`
}

func (r Result) Known() bool { return r.UnknownReason == "" && r.Cost != nil }

type Item struct {
	Model           string      `json:"model"`
	Tokens          TokenUsage  `json:"tokens"`
	CacheWriteKnown bool        `json:"cache_write_known"`
	HarnessMode     HarnessMode `json:"harness_mode,omitempty"`
}

type tokenRates struct {
	input      NanoUSD
	cached     NanoUSD
	cacheWrite NanoUSD
	output     NanoUSD
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
	canonical               string
	requiresCacheWrites     bool
	supportsLongContextFast bool
	longContextThreshold    int64
	short                   tokenRates
	long                    tokenRates
	fast                    multiplier
}

type Catalog struct {
	version Version
	basis   Basis
	models  map[string]modelRate
}

func (c Catalog) Version() Version { return c.version }

func (c Catalog) PriceItem(value Item) Result {
	model := strings.TrimSpace(value.Model)
	mode := value.HarnessMode
	if mode == "" {
		mode = HarnessStandard
	}
	result := Result{
		Model: model, RateVersion: c.version, Basis: c.basis, HarnessMode: mode,
	}
	if !validTokens(value.Tokens, value.CacheWriteKnown) {
		result.UnknownReason = UnknownInvalidTokens
		return result
	}
	rate, ok := c.models[model]
	if !ok {
		result.UnknownReason = UnknownModel
		return result
	}
	result.CanonicalModel = rate.canonical
	if rate.requiresCacheWrites && !value.CacheWriteKnown {
		result.UnknownReason = UnknownCacheWriteRequired
		return result
	}
	rates := rate.short
	result.Context = ShortContext
	if value.Tokens.InputTokens > rate.longContextThreshold {
		rates = rate.long
		result.Context = LongContext
	}
	scale := standard
	switch mode {
	case HarnessStandard:
	case HarnessFast:
		if rate.fast.denominator == 0 {
			result.UnknownReason = UnknownFastUnsupported
			return result
		}
		if result.Context == LongContext && !rate.supportsLongContextFast {
			result.UnknownReason = UnknownFastUnsupported
			return result
		}
		scale = rate.fast
	default:
		result.UnknownReason = UnknownHarnessMode
		return result
	}
	return priceAtRates(result, value.Tokens, rates, rate.requiresCacheWrites, scale)
}

func priceAtRates(
	result Result,
	tokens TokenUsage,
	rates tokenRates,
	separateCacheWrites bool,
	scale multiplier,
) Result {
	uncached := tokens.InputTokens - tokens.CachedInputTokens
	cacheWrite := int64(0)
	if separateCacheWrites {
		cacheWrite = tokens.CacheWriteTokens
		uncached -= cacheWrite
	}
	total := NanoUSD(0)
	for _, component := range []struct {
		tokens int64
		rate   NanoUSD
	}{
		{uncached, rates.input},
		{tokens.CachedInputTokens, rates.cached},
		{cacheWrite, rates.cacheWrite},
		{tokens.OutputTokens, rates.output},
	} {
		value, ok := multiplyCost(component.tokens, component.rate)
		if !ok {
			result.UnknownReason = UnknownCostOverflow
			return result
		}
		total, ok = addCost(total, value)
		if !ok {
			result.UnknownReason = UnknownCostOverflow
			return result
		}
	}
	var ok bool
	total, ok = scaleCost(total, scale)
	if !ok {
		result.UnknownReason = UnknownCostOverflow
		return result
	}
	result.Cost = &total
	return result
}

func scaleCost(value NanoUSD, scale multiplier) (NanoUSD, bool) {
	if value < 0 || scale.numerator <= 0 || scale.denominator <= 0 ||
		(value != 0 && int64(value) > math.MaxInt64/scale.numerator) {
		return 0, false
	}
	scaled := int64(value) * scale.numerator
	whole, remainder := scaled/scale.denominator, scaled%scale.denominator
	if remainder != 0 && remainder >= (scale.denominator+1)/2 {
		if whole == math.MaxInt64 {
			return 0, false
		}
		whole++
	}
	return NanoUSD(whole), true
}

func validTokens(value TokenUsage, cacheWriteKnown bool) bool {
	if value.InputTokens < 0 || value.CachedInputTokens < 0 || value.CacheWriteTokens < 0 ||
		value.OutputTokens < 0 || value.ReasoningTokens < 0 || value.TotalTokens < 0 {
		return false
	}
	if value.CachedInputTokens > value.InputTokens || value.ReasoningTokens > value.OutputTokens {
		return false
	}
	uncached := value.InputTokens - value.CachedInputTokens
	if cacheWriteKnown {
		if value.CacheWriteTokens > uncached {
			return false
		}
	} else if value.CacheWriteTokens != 0 {
		return false
	}
	if value.TotalTokens == 0 {
		return true
	}
	total, ok := addTokenCounts(value.InputTokens, value.OutputTokens)
	return ok && value.TotalTokens == total
}

func multiplyCost(tokens int64, rate NanoUSD) (NanoUSD, bool) {
	if tokens < 0 || rate < 0 || (rate != 0 && tokens > math.MaxInt64/int64(rate)) {
		return 0, false
	}
	return NanoUSD(tokens * int64(rate)), true
}

func addCost(left, right NanoUSD) (NanoUSD, bool) {
	if left < 0 || right < 0 || left > NanoUSD(math.MaxInt64)-right {
		return 0, false
	}
	return left + right, true
}

func addTokenCounts(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}
