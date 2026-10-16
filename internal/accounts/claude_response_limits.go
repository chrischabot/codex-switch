package accounts

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/joshp123/codex-switch/internal/store"
)

// ObserveClaudeResponse retains all reported windows, including a model bucket
// absent from the next side request. Account state uses shared windows only.
func (monitor *Monitor) ObserveClaudeResponse(name store.AccountName, model string, headers http.Header, status int) {
	now := time.Now()
	monitor.mutex.Lock()
	monitored := monitor.monitoredAccountLocked(name, ProviderClaude)
	fact := monitored.fact
	fact.Provider = ProviderClaude
	incoming := AllowanceSample{ObservedAt: now}
	for header := range headers {
		header = strings.ToLower(header)
		claim := strings.TrimSuffix(strings.TrimPrefix(header, "anthropic-ratelimit-unified-"), "-utilization")
		if !strings.HasPrefix(header, "anthropic-ratelimit-unified-") || !strings.HasSuffix(header, "-utilization") || claim == "overage" {
			continue
		}
		prefix := "anthropic-ratelimit-unified-" + claim
		used, err := strconv.ParseFloat(headers.Get(prefix+"-utilization"), 64)
		reset := claudeHeaderReset(headers.Get(prefix + "-reset"))
		if err != nil || reset.IsZero() {
			continue
		}
		window := ReportedWindow{UsedPercent: used * 100, ResetsAt: reset, DurationSeconds: 7 * 86400, ObservedAt: now,
			LimitReached: claudeStatusLimited(headers.Get(prefix + "-status"))}
		switch claim {
		case "5h":
			window.DurationSeconds = 5 * 3600
		case "7d":
		default:
			if !strings.HasPrefix(claim, "7d") || model == "" {
				continue
			}
			window.ModelName = claudeModelDisplayName(model)
			switch strings.ReplaceAll(claim, "-", "_") {
			case "7d_opus":
				window.ModelName = "Opus"
			case "7d_sonnet":
				window.ModelName = "Sonnet"
			case "7d_fable", "7d_oi":
				window.ModelName = "Fable"
			}
			for _, known := range fact.AllowanceSample.Windows {
				matchesScope := ClaudeModelFamily(known.ModelName) == ClaudeModelFamily(window.ModelName)
				if known.ModelID != "" {
					matchesScope = matchesScope && claudeWindowMatchesModel(known, model)
				}
				if matchesScope && known.ResetsAt.Equal(reset) {
					window.ModelID, window.ModelName = known.ModelID, known.ModelName
					break
				}
			}
		}
		if validateClaudeWindow(window) == nil {
			incoming.Windows = append(incoming.Windows, window)
		}
	}
	fact.AllowanceSample = mergeClaudeAllowance(fact.AllowanceSample, incoming)
	fact.ClaudeRestrictions = mergeClaudeRestrictions(nil, fact.ClaudeRestrictions)
	if status == http.StatusTooManyRequests {
		scopes := make(map[string]time.Time)
		for _, window := range incoming.Windows {
			if !window.LimitReached && window.UsedPercent < 100 {
				continue
			}
			scope := claudeWindowRestrictionScope(window)
			if window.ResetsAt.After(scopes[scope]) {
				scopes[scope] = window.ResetsAt
			}
		}
		claim := headers.Get("anthropic-ratelimit-unified-representative-claim")
		fallbackScope := canonicalClaudeModelID(model)
		if claim == "five_hour" || claim == "seven_day" {
			fallbackScope = ""
		}
		deadline := claudeHeaderReset(headers.Get("anthropic-ratelimit-unified-reset"))
		if retry := claudeRetryAfter(headers, now); retry.After(deadline) {
			deadline = retry
		}
		if len(scopes) == 0 || fallbackScope == "" {
			if !deadline.After(now) {
				deadline = now.Add(time.Minute)
			}
			if deadline.After(scopes[fallbackScope]) {
				scopes[fallbackScope] = deadline
			}
		}
		latestReset := deadline
		for scope, reset := range scopes {
			if deadline.After(reset) {
				reset = deadline
			}
			fact.ClaudeRestrictions[scope] = ClaudeRestriction{ResetsAt: reset, ObservedAt: now}
			if reset.After(latestReset) {
				latestReset = reset
			}
		}
		log.Printf("%s: Claude limit for %s; available after %s", fact.DisplayName, model, latestReset.Local().Format("Mon 2 Jan 15:04"))
	} else if status >= 200 && status < 300 {
		clearClaudeRestrictionsFromSample(&fact, incoming)
	}
	updateClaudeSharedState(&fact, now)
	availabilityChange := describeAccountAvailabilityChange(monitored.fact, fact, "")
	monitored.fact = fact
	monitor.mutex.Unlock()
	if availabilityChange != "" {
		log.Print(availabilityChange)
	}
	if err := monitor.saveClaudeAllowanceCache(string(name), claudeAllowanceCache{Sample: fact.AllowanceSample, Restrictions: fact.ClaudeRestrictions}); err != nil {
		log.Printf("%s: could not save Claude limits: %v", fact.DisplayName, err)
	}
}

func claudeModelDisplayName(model string) string {
	family := ClaudeModelFamily(model)
	if family == "" {
		return model
	}
	return strings.ToUpper(family[:1]) + family[1:]
}

func claudeStatusLimited(status string) bool { return status == "rejected" || status == "rate_limited" }

func claudeHeaderReset(value string) time.Time {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return time.Unix(seconds, 0)
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

func claudeRetryAfter(headers http.Header, now time.Time) time.Time {
	if seconds, err := strconv.ParseFloat(headers.Get("Retry-After"), 64); err == nil && seconds > 0 {
		return now.Add(time.Duration(seconds * float64(time.Second)))
	}
	parsed, _ := http.ParseTime(headers.Get("Retry-After"))
	return parsed
}
