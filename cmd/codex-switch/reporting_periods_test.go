package main

import (
	"testing"
	"time"
)

func TestReportingHistoryBoundsCoverThirtyUTCDaysThroughSnapshot(t *testing.T) {
	at := time.Date(2026, time.July, 15, 10, 30, 0, 123, time.UTC)
	start, end, err := reportingHistoryBounds(at, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, time.June, 16, 0, 0, 0, 0, time.UTC); !start.Equal(want) {
		t.Fatalf("start = %s, want %s", start, want)
	}
	if want := at.UTC().Add(time.Nanosecond); !end.Equal(want) {
		t.Fatalf("end = %s, want %s", end, want)
	}
}
