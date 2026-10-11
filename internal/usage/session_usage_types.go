package usage

import "time"

// taskMetadata is the lineage a session file declares for its task.
type taskMetadata struct {
	SourceKind       string
	TaskID           string
	ParentTaskID     string
	ForkedFromTaskID string
	CreatedAt        time.Time
}

type tokenUsage struct {
	InputTokens           int64
	CachedInputTokens     int64
	CacheWriteInputTokens *int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
}

type sessionAllowanceWindow struct {
	UsedPercent   float64
	WindowMinutes int64
	ResetsAt      time.Time
}

type sessionAllowance struct {
	LimitID   string
	PlanType  string
	Primary   *sessionAllowanceWindow
	Secondary *sessionAllowanceWindow
}

const piSessionSourceKind = "pi"

// sessionUsageObservation is one usage or allowance record from a local session file.
// Usage is nil for an allowance update which repeats previously reported token counts.
type sessionUsageObservation struct {
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
	Usage        *tokenUsage
	Allowance    *sessionAllowance
}

// sessionReadCursor contains only the parsing state needed to resume after the last complete line.
type sessionReadCursor struct {
	KnownParentTaskID  string
	Offset             int64
	PreviousTotals     *tokenUsage
	Model              string
	ServiceTier        *string
	TurnID             string
	ChildHistoryEnded  bool
	CanonicalUsageSeen bool
	PendingResponseID  string
	PendingUsage       *tokenUsage
}

type sessionFile struct {
	Path       string
	TaskID     string
	Size       int64
	ModifiedAt time.Time
	Identity   string
}

type sessionReadResult struct {
	Metadata     taskMetadata
	Cursor       sessionReadCursor
	Observations []sessionUsageObservation
	ChildTasks   []childTaskRecord
	BytesRead    int64
}

type childTaskRecord struct {
	TaskID       string
	SourceOffset int64
}

type sessionImportSummary struct {
	FilesDiscovered       int
	FilesRead             int
	UsageObservations     int
	AllowanceObservations int
	BytesRead             int64
}
