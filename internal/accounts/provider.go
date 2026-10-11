package accounts

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type usageResponse struct {
	PlanType     string                 `json:"plan_type"`
	RateLimit    *rateLimit             `json:"rate_limit"`
	ResetCredits *rateLimitResetCredits `json:"rate_limit_reset_credits"`
}

type rateLimitResetCredits struct {
	AvailableCount *uint32 `json:"available_count"`
}

type rateLimit struct {
	Allowed      *bool        `json:"allowed"`
	LimitReached *bool        `json:"limit_reached"`
	Primary      *usageWindow `json:"primary_window"`
	Secondary    *usageWindow `json:"secondary_window"`
}

type usageWindow struct {
	UsedPercent       *float64 `json:"used_percent"`
	DurationSeconds   *int64   `json:"limit_window_seconds"`
	ResetAfterSeconds *int64   `json:"reset_after_seconds"`
	ResetAt           *int64   `json:"reset_at"`
}

// Sample takes the immutable subscription identity from the credentials used
// for the request. The usage response's account_id currently duplicates its
// user_id and is not the ChatGPT-Account-ID request header.
func (u usageResponse) Sample(route, credentialAccountID string, observedAt time.Time) (AllowanceSample, error) {
	if u.RateLimit == nil || u.RateLimit.Allowed == nil || u.RateLimit.LimitReached == nil {
		return AllowanceSample{}, errors.New("usage response has no complete rate-limit state")
	}
	credentialAccountID = strings.TrimSpace(credentialAccountID)
	if credentialAccountID == "" {
		return AllowanceSample{}, errors.New("usage request has no authenticated subscription identity")
	}
	observedAt = observedAt.UTC()
	result := AllowanceSample{
		Route: route, AccountID: credentialAccountID, Plan: u.PlanType,
		Allowed: *u.RateLimit.Allowed, LimitReached: *u.RateLimit.LimitReached, ObservedAt: observedAt,
	}
	if u.ResetCredits != nil {
		result.AvailableResetCredits = u.ResetCredits.AvailableCount
	}
	for index, raw := range []*usageWindow{u.RateLimit.Primary, u.RateLimit.Secondary} {
		if raw == nil {
			continue
		}
		if raw.UsedPercent == nil || raw.DurationSeconds == nil || *raw.DurationSeconds <= 0 ||
			*raw.UsedPercent < 0 || *raw.UsedPercent > 100 {
			return AllowanceSample{}, fmt.Errorf("usage response window %d is incomplete or invalid", index+1)
		}
		reset := time.Time{}
		if raw.ResetAt != nil && *raw.ResetAt > 0 {
			reset = time.Unix(*raw.ResetAt, 0).UTC()
		} else if raw.ResetAfterSeconds != nil && *raw.ResetAfterSeconds > 0 {
			reset = observedAt.Add(time.Duration(*raw.ResetAfterSeconds) * time.Second).Truncate(time.Second)
		}
		if reset.IsZero() {
			return AllowanceSample{}, fmt.Errorf("usage response window %d has no reset time", index+1)
		}
		result.Windows = append(result.Windows, ReportedWindow{
			UsedPercent: *raw.UsedPercent, DurationSeconds: *raw.DurationSeconds, ResetsAt: reset,
		})
	}
	return result, nil
}
