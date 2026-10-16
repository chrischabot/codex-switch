package accounts

import (
	"testing"
	"time"
)

func TestReserveHoldsEntireAccountAtAnyCurrentWindow(t *testing.T) {
	now := time.Now()
	for _, window := range []ReportedWindow{
		{DurationSeconds: 5 * 3600},
		{DurationSeconds: 7 * 86400},
		{DurationSeconds: 7 * 86400, ModelName: "Opus"},
	} {
		window.ResetsAt = now.Add(time.Hour)
		fact := AccountRoutingFact{ReservePercent: 10, AllowanceSample: AllowanceSample{Windows: []ReportedWindow{window}}}
		for _, usage := range []struct {
			used float64
			held bool
		}{{89.9, false}, {90, true}, {90.1, true}} {
			fact.AllowanceSample.Windows[0].UsedPercent = usage.used
			if held := ReserveUnavailableReason(fact, now) != ""; held != usage.held {
				t.Fatalf("window %+v at %.1f%% used: held=%v", window, usage.used, held)
			}
		}
		fact.ReservePercent = 0
		if reason := ReserveUnavailableReason(fact, now); reason != "" {
			t.Fatalf("disabled reserve: %s", reason)
		}
		fact.ReservePercent = 10
		if reason := ReserveUnavailableReason(fact, window.ResetsAt); reason != "" {
			t.Fatalf("expired window: %s", reason)
		}
	}
}
