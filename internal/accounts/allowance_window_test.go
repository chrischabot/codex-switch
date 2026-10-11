package accounts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAnalyseUsesTheExactFiveHourWindow(t *testing.T) {
	start := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	result := analyse(sample("id-1", "pro", start.Add(2*time.Hour), ReportedWindow{
		UsedPercent: 40, DurationSeconds: int64((5 * time.Hour).Seconds()), ResetsAt: start.Add(5 * time.Hour),
	}), AccountReady, FiveHours)
	if !result.Known() {
		t.Fatalf("analysis = %#v", result)
	}
	window := result.Window
	if window.UsedPercent != 40 || window.FreePercent != 60 ||
		window.DurationSeconds != int64((5*time.Hour).Seconds()) ||
		!window.Period.StartedAt.Equal(start) || !window.ObservedAt.Equal(start.Add(2*time.Hour)) ||
		!window.Period.ResetsAt.Equal(start.Add(5*time.Hour)) {
		t.Fatalf("window = %#v", window)
	}
}

func TestAnalyseUsesTheExactWeeklyWindow(t *testing.T) {
	start := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	result := analyse(sample("id-1", "pro", start.Add(72*time.Hour), ReportedWindow{
		UsedPercent: 21, DurationSeconds: int64((7 * 24 * time.Hour).Seconds()), ResetsAt: start.Add(7 * 24 * time.Hour),
	}), AccountReady, Weekly)
	if !result.Known() || result.Window.UsedPercent != 21 || result.Window.FreePercent != 79 {
		t.Fatalf("analysis = %#v", result)
	}
}

func TestAnalyseReportsMissingAndResetWindowsAsUnknown(t *testing.T) {
	start := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	weeklyOnly := sample("id-1", "pro", start.Add(time.Hour), ReportedWindow{
		UsedPercent: 10, DurationSeconds: int64((7 * 24 * time.Hour).Seconds()), ResetsAt: start.Add(7 * 24 * time.Hour),
	})
	if got := analyse(weeklyOnly, AccountReady, FiveHours).UnknownReason; got != UnknownWindowNotReported {
		t.Fatalf("missing window = %q", got)
	}
	expired := sample("id-1", "pro", start.Add(5*time.Hour), ReportedWindow{
		UsedPercent: 10, DurationSeconds: int64((5 * time.Hour).Seconds()), ResetsAt: start.Add(5 * time.Hour),
	})
	if got := analyse(expired, AccountReady, FiveHours).UnknownReason; got != UnknownWindowAlreadyReset {
		t.Fatalf("expired window = %q", got)
	}
	atStart := sample("id-1", "pro", start, ReportedWindow{
		UsedPercent: 0, DurationSeconds: int64((5 * time.Hour).Seconds()), ResetsAt: start.Add(5 * time.Hour),
	})
	if got := analyse(atStart, AccountReady, FiveHours); !got.Known() || got.Window.UsedPercent != 0 || got.Window.FreePercent != 100 {
		t.Fatalf("window start = %#v", got)
	}
}

func TestAnalyseRejectsInvalidProviderWindowData(t *testing.T) {
	start := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		window ReportedWindow
	}{
		{name: "negative use", window: ReportedWindow{UsedPercent: -1, DurationSeconds: 18000, ResetsAt: start.Add(5 * time.Hour)}},
		{name: "use above 100", window: ReportedWindow{UsedPercent: 101, DurationSeconds: 18000, ResetsAt: start.Add(5 * time.Hour)}},
		{name: "missing duration", window: ReportedWindow{UsedPercent: 10, ResetsAt: start.Add(5 * time.Hour)}},
		{name: "missing reset", window: ReportedWindow{UsedPercent: 10, DurationSeconds: 18000}},
		{name: "conflicting durations", window: ReportedWindow{UsedPercent: 10, Duration: 4 * time.Hour, DurationSeconds: 18000, ResetsAt: start.Add(5 * time.Hour)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := analyse(sample("id-1", "pro", start.Add(time.Hour), test.window), AccountReady, FiveHours)
			if got.Known() || got.UnknownReason != UnknownInvalidWindow {
				t.Fatalf("invalid window = %#v", got)
			}
		})
	}
}

func TestPeriodUsesTheProviderWindowAsAHalfOpenBoundary(t *testing.T) {
	start := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	period := AllowancePeriod{StartedAt: start, ResetsAt: start.Add(5 * time.Hour)}
	for _, test := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{name: "before start", at: start.Add(-time.Nanosecond), want: false},
		{name: "at start", at: start, want: true},
		{name: "inside", at: start.Add(time.Hour), want: true},
		{name: "before reset", at: start.Add(5*time.Hour - time.Nanosecond), want: true},
		{name: "at reset", at: start.Add(5 * time.Hour), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := period.Contains(test.at); got != test.want {
				t.Fatalf("Contains(%s) = %v, want %v", test.at, got, test.want)
			}
		})
	}
}

func TestAnalyseReportsProviderAccountStates(t *testing.T) {
	start := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	value := sample("id-1", "pro", start.Add(2*time.Hour), ReportedWindow{
		UsedPercent: 40, DurationSeconds: 18000, ResetsAt: start.Add(5 * time.Hour),
	})
	for _, test := range []struct {
		state AccountState
		want  UnknownReason
	}{
		{state: AccountUsageCheckFailed, want: UnknownUsageCheckFailed},
		{state: AccountUsageCheckPending, want: UnknownUsageCheckPending},
		{state: AccountSignInRequired, want: UnknownSignInRequired},
		{state: AccountCredentialsUnavailable, want: UnknownCredentialsUnavailable},
	} {
		got := analyse(value, test.state, FiveHours)
		if got.Known() || got.UnknownReason != test.want {
			t.Fatalf("state %s = %#v", test.state, got)
		}
	}
}

func TestAnalyseUsesAsOfOnlyToRejectAnExpiredRetainedSample(t *testing.T) {
	start := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	value := sample("id-1", "pro", start.Add(2*time.Hour), ReportedWindow{
		UsedPercent: 40, DurationSeconds: 18000, ResetsAt: start.Add(5 * time.Hour),
	})
	current := AnalyseAllowance(AllowanceAnalysisInput{Sample: value, State: AccountReady, AsOf: start.Add(4 * time.Hour)}, FiveHours)
	if !current.Known() || current.Window.UsedPercent != 40 || current.Window.FreePercent != 60 {
		t.Fatalf("current sample = %#v", current)
	}
	expired := AnalyseAllowance(AllowanceAnalysisInput{Sample: value, State: AccountReady, AsOf: start.Add(5 * time.Hour)}, FiveHours)
	if expired.Known() || expired.UnknownReason != UnknownWindowAlreadyReset {
		t.Fatalf("expired retained sample = %#v", expired)
	}
}

func TestJSONDistinguishesUnknownFromKnownZeroValues(t *testing.T) {
	start := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	missing := analyse(sample("id-1", "pro", start.Add(time.Hour)), AccountReady, FiveHours)
	data, err := json.Marshal(missing)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"window"`) || !strings.Contains(string(data), `"unknown_reason":"window-not-reported"`) {
		t.Fatalf("unknown JSON = %s", data)
	}
	zero := analyse(sample("id-1", "pro", start.Add(time.Hour), ReportedWindow{
		UsedPercent: 0, DurationSeconds: 18000, ResetsAt: start.Add(5 * time.Hour),
	}), AccountReady, FiveHours)
	data, err = json.Marshal(zero)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"used_percent":0`, `"free_percent":100`, `"duration_seconds":18000`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("known zero JSON missing %s: %s", want, data)
		}
	}
	if strings.Contains(string(data), `"projection"`) || strings.Contains(string(data), `"trend"`) {
		t.Fatalf("factual window contains inferred fields: %s", data)
	}
}

func sample(accountID, plan string, observedAt time.Time, windows ...ReportedWindow) AllowanceSample {
	return AllowanceSample{
		Route: "route-" + accountID, AccountID: accountID, Plan: plan,
		ObservedAt: observedAt, Windows: windows,
	}
}

func analyse(value AllowanceSample, state AccountState, kind AllowanceWindowKind) AllowanceResult {
	return AnalyseAllowance(AllowanceAnalysisInput{Sample: value, State: state, AsOf: value.ObservedAt}, kind)
}
