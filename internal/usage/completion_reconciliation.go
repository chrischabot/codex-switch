package usage

import (
	"fmt"
	"time"

	"github.com/josh/codex-switch/internal/store"
)

// reconciledCompletion combines one native usage event with its serving response when available.
type reconciledCompletion struct {
	TaskID            string
	At                time.Time
	AccountName       store.AccountName
	ProviderAccountID store.ProviderAccountID
	Model             string
	Tokens            int64
	Cost              *nanoUSD
	NativeSource      *sessionUsageObservation
	ResponseID        store.CompletedResponseID
}

// reconcileCompletions counts native and proxy observations of the same response once.
// Provider response IDs identify the work across both sources.
func reconcileCompletions(native []sessionUsageObservation, responses []store.CompletedResponseRecord) ([]reconciledCompletion, error) {
	byResponseID := make(map[store.CompletedResponseID]store.CompletedResponseRecord)
	firstResponseByTask := make(map[string]time.Time)
	for _, response := range responses {
		byResponseID[response.CompletedResponseID] = response
		taskID := string(response.CodexTaskID)
		if first := firstResponseByTask[taskID]; first.IsZero() || response.CompletedAt.Before(first) {
			firstResponseByTask[taskID] = response.CompletedAt
		}
	}
	matched := make(map[store.CompletedResponseID]bool)
	var completions []reconciledCompletion
	for index := range native {
		observation := &native[index]
		if observation.Usage == nil {
			continue
		}
		responseID := store.CompletedResponseID(observation.ResponseID)
		if responseID == "" {
			if first := firstResponseByTask[observation.TaskID]; !first.IsZero() && !observation.At.Before(first) {
				return nil, fmt.Errorf("native usage without response identity overlaps proxy history: %s at %d", observation.SourcePath, observation.SourceOffset)
			}
		} else {
			if matched[responseID] {
				continue
			}
			matched[responseID] = true
		}
		usage := *observation.Usage
		completion := reconciledCompletion{TaskID: observation.TaskID, At: observation.At, Model: observation.Model, Tokens: usage.InputTokens + usage.OutputTokens, NativeSource: observation, ResponseID: responseID}
		mode := harnessStandard
		if observation.SourceKind == piSessionSourceKind {
			mode = ""
		}
		if observation.ServiceTier != nil {
			switch *observation.ServiceTier {
			case "default", "":
				mode = harnessStandard
			case "priority", "fast":
				mode = harnessFast
			default:
				mode = harnessMode("unsupported")
			}
		}
		if response, found := byResponseID[responseID]; found {
			if usage.InputTokens != response.InputTokens || usage.CachedInputTokens != response.CachedInputTokens || usage.OutputTokens != response.OutputTokens || usage.ReasoningOutputTokens != response.ReasoningTokens {
				return nil, fmt.Errorf("token counts disagree for response %s", responseID)
			}
			completion.AccountName = response.AccountName
			completion.Model = response.ModelSlug
			if response.HarnessMode == "standard" || response.HarnessMode == "fast" {
				mode = harnessMode(response.HarnessMode)
			}
			if response.CacheWriteInputTokens != nil {
				if usage.CacheWriteInputTokens != nil && *usage.CacheWriteInputTokens != *response.CacheWriteInputTokens {
					return nil, fmt.Errorf("cache-write counts disagree for response %s", response.CompletedResponseID)
				}
				usage.CacheWriteInputTokens = response.CacheWriteInputTokens
			}
		}
		completion.Cost = apiEquivalentPrice(completion.Model, mode, usage)
		completions = append(completions, completion)
	}
	for _, response := range responses {
		if matched[response.CompletedResponseID] {
			continue
		}
		usage := tokenUsage{InputTokens: response.InputTokens, CachedInputTokens: response.CachedInputTokens, CacheWriteInputTokens: response.CacheWriteInputTokens, OutputTokens: response.OutputTokens, ReasoningOutputTokens: response.ReasoningTokens}
		completions = append(completions, reconciledCompletion{TaskID: string(response.CodexTaskID), At: response.CompletedAt, AccountName: response.AccountName, Model: response.ModelSlug, Tokens: response.InputTokens + response.OutputTokens, Cost: apiEquivalentPrice(response.ModelSlug, harnessMode(response.HarnessMode), usage), ResponseID: response.CompletedResponseID})
	}
	return completions, nil
}
