package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestPluginHookOnlyClaimHasDaemonCredential(t *testing.T) {
	runtime := db.AgentRuntime{
		WorkspaceID: parseUUID(testWorkspaceID),
		DaemonID:    strToText("plugin-only-daemon"),
	}
	response := AgentTaskResponse{PluginHookTools: []service.PluginHookTool{{Name: "memory-recall"}}}
	raw, params, err := remoteMCPDaemonTokenForClaim(response, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "mdt_") || len(params) != 1 {
		t.Fatal("plugin-only claims must carry a persisted daemon credential")
	}
	if params[0].TokenHash != auth.HashToken(raw) || params[0].WorkspaceID != runtime.WorkspaceID || params[0].DaemonID != runtime.DaemonID.String {
		t.Fatal("plugin hook credential must remain bound to this workspace and daemon")
	}
	if remaining := time.Until(params[0].ExpiresAt.Time); remaining < 23*time.Hour || remaining > 24*time.Hour {
		t.Fatalf("unexpected credential lifetime: %s", remaining)
	}
	if _, _, err := remoteMCPDaemonTokenForClaim(response, db.AgentRuntime{WorkspaceID: runtime.WorkspaceID}); err == nil {
		t.Fatal("plugin-only claims without a daemon identity must fail closed")
	}
	if raw, params, err := remoteMCPDaemonTokenForClaim(AgentTaskResponse{}, runtime); err != nil || raw != "" || len(params) != 0 {
		t.Fatal("claims without plugin or remote MCP tools must not mint a credential")
	}
}
