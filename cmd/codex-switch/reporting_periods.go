package main

import (
	"fmt"
	"time"
)

// reportingHistoryBounds is the one history-read envelope required by the
// Today, 7-calendar-day and 30-calendar-day reports. The store's end is
// exclusive, so one nanosecond includes an event stamped at the snapshot.
func reportingHistoryBounds(at time.Time, location *time.Location) (time.Time, time.Time, error) {
	if at.IsZero() || location == nil {
		return time.Time{}, time.Time{}, fmt.Errorf("Codex reporting history requires a snapshot time")
	}
	local := at.In(location)
	localMidnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return localMidnight.AddDate(0, 0, -29).UTC(), at.UTC().Add(time.Nanosecond), nil
}
