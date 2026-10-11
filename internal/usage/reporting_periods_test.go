package usage

import (
	"testing"

	"github.com/josh/codex-switch/internal/accounts"
)

// Shares and costs follow account 1 on 1 and 2 October 2026: OpenAI's share
// for a model served only through this Mac gives one value per percent; a
// model also served by another machine gives a lower one.
func TestAllowanceValueIsTheOneModelDaysServedOnlyHereAgreeOn(t *testing.T) {
	value := 3_743_878_563.6 // nano-USD per percent
	shares := []accounts.OpenAIAllowanceShare{
		{Date: "2026-10-01", Model: "gpt-5.6-sol", Percent: 1.0468},
		{Date: "2026-10-02", Model: "gpt-5.6-sol", Percent: 1.8042},
		{Date: "2026-10-02", Model: "gpt-6-astra", Percent: 6.26},
		{Date: "2026-10-02", Model: "gpt-6-astra", Fast: true, Percent: 1},
	}
	local := map[modelDay]nanoUSD{
		{"2026-10-01", "gpt-5.6-sol", false}: nanoUSD(1.0468 * value),
		{"2026-10-02", "gpt-5.6-sol", false}: nanoUSD(1.8042 * value),
		{"2026-10-02", "gpt-6-astra", false}: nanoUSD(2.0 * value),
	}
	got, agreeing := allowanceValuePerPercent(shares, local)
	if agreeing != 2 || got < value*0.999999 || got > value*1.000001 {
		t.Fatalf("value = %f agreed by %d, want %f agreed by 2", got, agreeing, value)
	}
	costs := openAIDailyCost(shares, value)
	if want := nanoUSD((1.8042 + 6.26 + 1/fastAllowanceMultiplier*fastCostMultiplier) * value); costs["2026-10-02"] < want-2 || costs["2026-10-02"] > want+2 {
		t.Fatalf("2 October cost = %d, want %d", costs["2026-10-02"], want)
	}
}

// OpenAI's count wins once it has caught up; this Mac's count wins while
// OpenAI still reports only the first hours of the day.
func TestEachDayTakesTheLargerOfOpenAIAndThisMac(t *testing.T) {
	merged := accountDailyUsage(
		map[string]int64{"2026-10-10": 7_807_800, "2026-10-11": 946_491},
		map[string]nanoUSD{"2026-10-10": 90},
		map[string]usageAmount{"2026-10-10": {1_628_511, 20}, "2026-10-11": {34_138_059, 400}},
	)
	if got := merged["2026-10-10"]; got != (usageAmount{7_807_800, 90}) {
		t.Fatalf("finished day = %+v", got)
	}
	if got := merged["2026-10-11"]; got != (usageAmount{34_138_059, 400}) {
		t.Fatalf("day in progress = %+v", got)
	}
}
