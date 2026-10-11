package codexstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("Codex task details are unavailable")

type Reader interface {
	Lookup(context.Context, []string) (map[string]TaskMetadata, error)
}

type LocalReader struct {
	AppServerSocket  string
	CodexHome        string
	AppServerTimeout time.Duration
	CodexExecutable  string
	configCommand    func(context.Context) *exec.Cmd
}

func DefaultReader() (*LocalReader, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	reader := &LocalReader{
		AppServerSocket:  filepath.Join(codexHome, "app-server-control", "app-server-control.sock"),
		CodexHome:        codexHome,
		AppServerTimeout: 250 * time.Millisecond,
	}
	reader.CodexExecutable = FindCodexExecutable()
	return reader, nil
}

// FindCodexExecutable prefers the client bundled with the desktop app, so
// Codex Switch speaks the same client version as the app it serves.
func FindCodexExecutable() string {
	const desktopBundledCodex = "/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex"
	if _, err := os.Stat(desktopBundledCodex); err == nil {
		return desktopBundledCodex
	}
	return "codex"
}

func (r LocalReader) Lookup(ctx context.Context, taskIDs []string) (map[string]TaskMetadata, error) {
	requested := canonicalTaskIDs(taskIDs)
	if len(requested) == 0 {
		return map[string]TaskMetadata{}, nil
	}

	var appMetadata map[string]TaskMetadata
	appServerReady := false
	appErr := errors.New("app-server socket is not configured")
	if r.AppServerSocket != "" {
		appContext := ctx
		cancel := func() {}
		if r.AppServerTimeout > 0 {
			appContext, cancel = context.WithTimeout(ctx, r.AppServerTimeout)
		}
		appMetadata, appServerReady, appErr = r.lookupAppServer(appContext, requested)
		cancel()
	}
	fallbackTargets := metadataFallbackTargets(requested, appMetadata)
	if appErr == nil && len(fallbackTargets) == 0 {
		return appMetadata, nil
	}
	fileMetadata, filesAvailable, fileErr := r.lookupFiles(ctx, fallbackTargets)
	if appServerReady {
		fileMetadata = relevantFileMetadata(requested, appMetadata, fileMetadata)
	}
	metadata := mergeMetadata(appMetadata, fileMetadata)
	if err := ctx.Err(); err != nil {
		return metadata, err
	}
	missing := missingMetadata(requested, metadata)
	if len(missing) == 0 {
		return metadata, nil
	}

	problems := make([]string, 0, 2)
	if appErr != nil {
		problems = append(problems, "app-server: "+appErr.Error())
	} else if appServerReady {
		problems = append(problems, "app-server did not return every task detail")
	} else {
		problems = append(problems, "app-server is unavailable")
	}
	if fileErr != nil {
		problems = append(problems, "local task files: "+fileErr.Error())
	} else if !filesAvailable {
		problems = append(problems, "local task files are unavailable")
	} else {
		problems = append(problems, "local task files did not contain every task detail")
	}
	return metadata, unresolvedTaskDetails(missing, strings.Join(problems, "; "))
}

func relevantFileMetadata(
	requested []string,
	appMetadata, fileMetadata map[string]TaskMetadata,
) map[string]TaskMetadata {
	selected := make(map[string]TaskMetadata, len(fileMetadata))
	queue := make([]string, 0, len(requested)+len(appMetadata))
	for taskID := range appMetadata {
		queue = append(queue, taskID)
	}
	for _, taskID := range missingMetadata(requested, appMetadata) {
		queue = append(queue, taskID)
	}
	for len(queue) > 0 {
		taskID := queue[0]
		queue = queue[1:]
		if _, seen := selected[taskID]; seen {
			continue
		}
		value, ok := fileMetadata[taskID]
		if !ok {
			continue
		}
		selected[taskID] = value
		if _, appServerOwnsTask := appMetadata[taskID]; !appServerOwnsTask &&
			value.Relationship == TaskRelationshipKnownParent && value.ParentTaskID != "" {
			queue = append(queue, value.ParentTaskID)
		}
	}
	return selected
}

func unresolvedTaskDetails(missing []string, problem string) error {
	count := fmt.Sprintf("%d task detail records remain", len(missing))
	if len(missing) == 1 {
		count = "1 task detail record remains"
	}
	return unavailable(fmt.Errorf("%s unresolved; %s", count, problem))
}

func mergeMetadata(primary, fallback map[string]TaskMetadata) map[string]TaskMetadata {
	result := make(map[string]TaskMetadata, len(primary)+len(fallback))
	for taskID, value := range fallback {
		result[taskID] = value
	}
	for taskID, value := range primary {
		if existing, ok := result[taskID]; ok {
			if value.DisplayName == "" {
				value.DisplayName = existing.DisplayName
			}
			if value.AgentNickname == "" {
				value.AgentNickname = existing.AgentNickname
			}
			if relationshipUnknown(value.Relationship) && !relationshipUnknown(existing.Relationship) {
				value.Relationship = existing.Relationship
				value.ParentTaskID = existing.ParentTaskID
			} else if value.Relationship == TaskRelationshipKnownParent && value.ParentTaskID == "" &&
				existing.Relationship == TaskRelationshipKnownParent {
				value.ParentTaskID = existing.ParentTaskID
			}
			if value.RuntimeStatus.State == "" {
				value.RuntimeStatus = existing.RuntimeStatus
			}
			value.Sources = mergeSources(value.Sources, existing.Sources)
		}
		result[taskID] = value
	}
	return result
}

func metadataFallbackTargets(requested []string, metadata map[string]TaskMetadata) []string {
	targets := make(map[string]struct{}, len(requested)+len(metadata))
	for _, taskID := range missingMetadata(requested, metadata) {
		targets[taskID] = struct{}{}
	}
	for taskID, value := range metadata {
		if value.TaskID != taskID {
			continue
		}
		if value.DisplayName == "" || relationshipUnknown(value.Relationship) ||
			(value.Relationship == TaskRelationshipKnownParent &&
				(value.ParentTaskID == "" || value.AgentNickname == "")) {
			targets[taskID] = struct{}{}
		}
	}
	result := make([]string, 0, len(targets))
	for taskID := range targets {
		result = append(result, taskID)
	}
	sort.Strings(result)
	return result
}

func mergeSources(primary, fallback []TaskMetadataSource) []TaskMetadataSource {
	seen := make(map[TaskMetadataSource]struct{}, len(primary)+len(fallback))
	for _, source := range append(append([]TaskMetadataSource(nil), primary...), fallback...) {
		if source != "" {
			seen[source] = struct{}{}
		}
	}
	result := make([]TaskMetadataSource, 0, len(seen))
	for _, source := range []TaskMetadataSource{
		TaskMetadataFromAppServer,
		TaskMetadataFromLocalFiles,
	} {
		if _, ok := seen[source]; ok {
			result = append(result, source)
			delete(seen, source)
		}
	}
	for source := range seen {
		result = append(result, source)
	}
	return result
}

func missingMetadata(requested []string, metadata map[string]TaskMetadata) []string {
	targets := make(map[string]struct{}, len(requested))
	for _, taskID := range requested {
		targets[taskID] = struct{}{}
	}
	for changed := true; changed; {
		changed = false
		for taskID := range targets {
			value, ok := metadata[taskID]
			if !ok || value.Relationship != TaskRelationshipKnownParent || value.ParentTaskID == "" {
				continue
			}
			if _, exists := targets[value.ParentTaskID]; !exists {
				targets[value.ParentTaskID] = struct{}{}
				changed = true
			}
		}
	}
	missing := make([]string, 0, len(targets))
	for taskID := range targets {
		if value, ok := metadata[taskID]; !ok || value.TaskID != taskID {
			missing = append(missing, taskID)
		}
	}
	sort.Strings(missing)
	return missing
}

func relationshipUnknown(value TaskRelationshipState) bool {
	return value == "" || value == TaskRelationshipUnknown
}

func canonicalTaskIDs(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		if canonicalTaskID(value) == value {
			seen[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func unavailable(err error) error {
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}
