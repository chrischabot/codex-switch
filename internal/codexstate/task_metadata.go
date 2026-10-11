package codexstate

import "strings"

// TaskMetadata is what Codex itself knows about one task: its title, its place
// in the task tree, and its live runtime status when the app-server reports it.
type TaskMetadata struct {
	TaskID        string
	DisplayName   string
	AgentNickname string
	ParentTaskID  string
	Relationship  TaskRelationshipState
	RuntimeStatus TaskRuntimeStatus
	Sources       []TaskMetadataSource
}

// TaskRelationshipState records what the metadata source proved about a
// task's place in the task tree. ParentTaskID is meaningful only for a known
// parent relationship.
type TaskRelationshipState string

const (
	TaskRelationshipUnknown     TaskRelationshipState = "unknown"
	TaskRelationshipKnownRoot   TaskRelationshipState = "known-root"
	TaskRelationshipKnownParent TaskRelationshipState = "known-parent"
)

// TaskMetadataSource states where the read-only task facts came from. Local
// files are limited to Codex's public session index and the first session_meta
// record; they do not provide live runtime status.
type TaskMetadataSource string

const (
	TaskMetadataFromAppServer  TaskMetadataSource = "app-server"
	TaskMetadataFromLocalFiles TaskMetadataSource = "local-files"
)

// TaskRuntimeState is the read-only state reported by Codex app-server's
// Thread.status field. The empty value means that the file fallback, or a
// newer app-server state, could not provide this evidence.
type TaskRuntimeState string

const (
	TaskRuntimeNotLoaded   TaskRuntimeState = "notLoaded"
	TaskRuntimeIdle        TaskRuntimeState = "idle"
	TaskRuntimeSystemError TaskRuntimeState = "systemError"
	TaskRuntimeActive      TaskRuntimeState = "active"
)

type TaskActiveFlag string

const (
	TaskWaitingOnApproval TaskActiveFlag = "waitingOnApproval"
	TaskWaitingOnUser     TaskActiveFlag = "waitingOnUserInput"
)

type TaskRuntimeStatus struct {
	State       TaskRuntimeState `json:"state,omitempty"`
	ActiveFlags []TaskActiveFlag `json:"active_flags,omitempty"`
}

// canonicalTaskID returns the lowercase task UUID from a Codex identifier. The
// UUID is the whole value or the part after its last colon; any other value
// returns "".
func canonicalTaskID(value string) string {
	if index := strings.LastIndexByte(value, ':'); index >= 0 {
		value = value[index+1:]
	}
	value = strings.ToLower(value)
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return ""
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ""
		}
	}
	return value
}
