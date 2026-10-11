package store

import "time"

// AccountName keeps durable local identity separate from provider account IDs and emails.
type AccountName string

// CodexTaskID makes the affinity key impossible to swap with another string identifier.
type CodexTaskID string

// WorkKind prevents metadata requests from creating or moving model-turn pins.
type WorkKind string

const (
	ModelTurnWorkKind    WorkKind = "model_turn"
	TaskMetadataWorkKind WorkKind = "task_metadata"
)

// TaskAccountPin is durable so draining proxy generations agree on the next account.
type TaskAccountPin struct {
	CodexTaskID CodexTaskID
	AccountName AccountName
	PinnedAt    time.Time
}

// CompletedResponseID is the provider identity used to record one completion exactly once.
type CompletedResponseID string

// CompletedResponseRecord excludes response content because accounting needs token facts only.
type CompletedResponseRecord struct {
	CompletedResponseID   CompletedResponseID
	CodexTaskID           CodexTaskID
	AccountName           AccountName
	ModelSlug             string
	HarnessMode           string
	InputTokens           int64
	CachedInputTokens     int64
	CacheWriteInputTokens *int64
	OutputTokens          int64
	ReasoningTokens       int64
	CompletedAt           time.Time
}
