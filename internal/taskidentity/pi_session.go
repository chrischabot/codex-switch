package taskidentity

import (
	"crypto/sha256"
	"fmt"
)

// PiSession uses the same task identity for routing and native usage import.
func PiSession(sessionID string) string {
	identity := sha256.Sum256([]byte("codex-switch/pi-session/" + sessionID))
	identity[6] = identity[6]&0x0f | 0x80
	identity[8] = identity[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", identity[:4], identity[4:6], identity[6:8], identity[8:10], identity[10:16])
}

// IsPiSession reports whether taskID came from PiSession, which marks its IDs
// as version 8 UUIDs. Codex task IDs are version 7 UUIDs.
func IsPiSession(taskID string) bool {
	return len(taskID) == 36 && taskID[14] == '8'
}
