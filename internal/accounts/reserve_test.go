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
		fact := AccountRoutingFact{Reserve: AccountReserve{SoftPercent: 15, HardPercent: 10}, AllowanceSample: AllowanceSample{Windows: []ReportedWindow{window}}}
		for _, usage := range []struct {
			used float64
			soft bool
			held bool
		}{{84.9, false, false}, {85, true, false}, {89.9, true, false}, {90, true, true}, {90.1, true, true}} {
			fact.AllowanceSample.Windows[0].UsedPercent = usage.used
			if soft := SoftReserveReached(fact, now); soft != usage.soft {
				t.Fatalf("window %+v at %.1f%% used: soft reserve=%v", window, usage.used, soft)
			}
			if held := ReserveUnavailableReason(fact, now) != ""; held != usage.held {
				t.Fatalf("window %+v at %.1f%% used: held=%v", window, usage.used, held)
			}
		}
		fact.Reserve = AccountReserve{}
		if reason := ReserveStatus(fact, now); reason != "" {
			t.Fatalf("disabled reserve: %s", reason)
		}
		fact.Reserve = AccountReserve{SoftPercent: 15, HardPercent: 10}
		if reason := ReserveStatus(fact, window.ResetsAt); reason != "" {
			t.Fatalf("expired window: %s", reason)
		}
	}
}
