// Package codexusage reads native Codex accounting observations without assigning accounts or prices.
package codexusage

import "time"

type TaskMetadata struct {
	SourceKind       string
	TaskID           string
	ParentTaskID     string
	ForkedFromTaskID string
	CreatedAt        time.Time
}

type TokenUsage struct {
	InputTokens           int64
	CachedInputTokens     int64
	CacheWriteInputTokens *int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
}

type AllowanceWindow struct {
	UsedPercent   float64
	WindowMinutes int64
	ResetsAt      time.Time
}

type AllowanceObservation struct {
	LimitID   string
	PlanType  string
	Primary   *AllowanceWindow
	Secondary *AllowanceWindow
}

const PiSessionSource = "pi"

// Usage is nil for an allowance update which repeats previously reported token counts.
type Observation struct {
	SourceKind   string
	ResponseID   string
	RootTurnID   string
	SessionID    string
	TaskID       string
	TurnID       string
	SourcePath   string
	SourceOffset int64
	At           time.Time
	Model        string
	ServiceTier  *string
	Usage        *TokenUsage
	Allowance    *AllowanceObservation
}

// Cursor contains only the parsing state needed to resume after the last complete line.
type Cursor struct {
	KnownParentTaskID  string
	Offset             int64
	PreviousTotals     *TokenUsage
	Model              string
	ServiceTier        *string
	TurnID             string
	ChildHistoryEnded  bool
	CanonicalUsageSeen bool
	PendingResponseID  string
	PendingUsage       *TokenUsage
}

type SourceFile struct {
	Path       string
	TaskID     string
	Size       int64
	ModifiedAt time.Time
	Identity   string
}

type ReadResult struct {
	Metadata     TaskMetadata
	Cursor       Cursor
	Observations []Observation
	ChildTasks   []ChildTaskRecord
	BytesRead    int64
}

type ChildTaskRecord struct {
	TaskID       string
	SourceOffset int64
}

type ImportResult struct {
	FilesDiscovered       int
	FilesRead             int
	UsageObservations     int
	AllowanceObservations int
	BytesRead             int64
}
