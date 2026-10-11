package accounts

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRawUsageShape(t *testing.T) {
	observed := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	var raw UsageResponse
	if err := json.Unmarshal([]byte(`{"user_id":"redacted","account_id":"redacted","email":"a@example.com","plan_type":"pro","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":54,"limit_window_seconds":18000,"reset_after_seconds":120,"reset_at":0}}}`), &raw); err != nil {
		t.Fatal(err)
	}
	s, err := raw.Sample("account-1", "redacted", observed)
	if err != nil {
		t.Fatal(err)
	}
	if s.AccountID != "redacted" || s.Email != "a@example.com" || !s.Allowed || s.LimitReached || len(s.Windows) != 1 || s.Windows[0].DurationSeconds != 18000 || !s.Windows[0].ResetsAt.Equal(observed.Add(120*time.Second)) {
		t.Fatalf("%#v", s)
	}
}

func TestUsageResponseRequiresExplicitRateLimitState(t *testing.T) {
	for _, data := range []string{`{}`, `{"rate_limit":{}}`, `{"rate_limit":{"allowed":true}}`} {
		var raw UsageResponse
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Sample("account-1", "account-id", time.Now()); err == nil {
			t.Fatalf("%s was accepted", data)
		}
	}
}

func TestUsageResponseRejectsIncompleteWindows(t *testing.T) {
	for _, data := range []string{
		`{"account_id":"id-1","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"limit_window_seconds":18000,"reset_at":1784487709}}}`,
		`{"account_id":"id-1","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":0,"reset_at":1784487709}}}`,
		`{"account_id":"id-1","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":0,"limit_window_seconds":18000}}}`,
	} {
		var raw UsageResponse
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Sample("account-1", "id-1", time.Now()); err == nil {
			t.Fatalf("incomplete window was accepted: %s", data)
		}
	}
	var raw UsageResponse
	if err := json.Unmarshal([]byte(`{"account_id":"provider-user-id","rate_limit":{"allowed":true,"limit_reached":false}}`), &raw); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Sample("account-1", "", time.Now()); err == nil {
		t.Fatal("missing authenticated subscription identity was accepted")
	}
	sample, err := raw.Sample("account-1", "subscription-id", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if sample.AccountID != "subscription-id" {
		t.Fatalf("sample account identity = %q", sample.AccountID)
	}
}

func TestUsageResponseAcceptsTheCurrentWeeklyOnlyShape(t *testing.T) {
	var raw UsageResponse
	if err := json.Unmarshal([]byte(`{
		"account_id":"provider-user-a",
		"email":"one@example.com",
		"plan_type":"pro",
		"rate_limit":{
			"allowed":true,
			"limit_reached":false,
			"primary_window":{"used_percent":24,"limit_window_seconds":604800,"reset_after_seconds":509187,"reset_at":1784487709},
			"secondary_window":null
		}
	}`), &raw); err != nil {
		t.Fatal(err)
	}
	sample, err := raw.Sample("account-1", "subscription-a", time.Date(2026, 7, 13, 21, 34, 22, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(sample.Windows) != 1 || sample.Windows[0].DurationSeconds != 604800 || sample.Windows[0].UsedPercent != 24 ||
		sample.AccountID != "subscription-a" || sample.Plan != "pro" {
		t.Fatalf("weekly-only sample = %#v", sample)
	}
}

func TestResetCreditsUseAvailableCount(t *testing.T) {
	for _, test := range []struct {
		name, resetField string
		want             *uint32
	}{
		{"stored credits remain available when not applicable", `,"rate_limit_reset_credits":{"available_count":2,"applicable_available_count":0}`, uint32Pointer(2)},
		{"observed zero", `,"rate_limit_reset_credits":{"available_count":0}`, uint32Pointer(0)},
		{"not reported", ``, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var response UsageResponse
			body := `{"plan_type":"free","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":25,"limit_window_seconds":2592000,"reset_at":1900000000}}` + test.resetField + `}`
			if err := json.Unmarshal([]byte(body), &response); err != nil {
				t.Fatal(err)
			}
			sample, err := response.Sample("account-1", "subscription", time.Unix(1899990000, 0))
			if err != nil {
				t.Fatal(err)
			}
			if sample.Plan != "free" || len(sample.Windows) != 1 || sample.Windows[0].DurationSeconds != 2592000 {
				t.Fatalf("monthly allowance lost: %#v", sample.Windows)
			}
			if (sample.AvailableResetCredits == nil) != (test.want == nil) {
				t.Fatal("reset credit presence lost")
			}
			if test.want != nil && *sample.AvailableResetCredits != *test.want {
				t.Fatalf("credits=%d", *sample.AvailableResetCredits)
			}
		})
	}
}

func uint32Pointer(value uint32) *uint32 { return &value }
