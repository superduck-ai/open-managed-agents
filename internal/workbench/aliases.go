package workbench

import "github.com/superduck-ai/open-managed-agents/internal/platform"

var ErrNotFound = platform.ErrNotFound
var ResolveWorkspaceScope = platform.ResolveWorkspaceScope

type ConsoleWorkspace = platform.ConsoleWorkspace
type WorkspaceScope = platform.WorkspaceScope
type WorkbenchPromptRecord = platform.WorkbenchPromptRecord
type WorkbenchRevisionRecord = platform.WorkbenchRevisionRecord
type WorkbenchKVRecord = platform.WorkbenchKVRecord
type WorkbenchEvaluationRecord = platform.WorkbenchEvaluationRecord
