package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// An agent-trigger tool is called from inside one run. The host knows which
// run from the claimed task row, so it tells the handler — in the signed body,
// not in tool input the model writes — and narrows the callback grant's task
// reads to that run. Issue reads are deliberately NOT narrowed: agent tools
// have always been able to act on other issues, and must keep working.
func TestAgentHookCarriesItsRunAndNarrowsOnlyTaskReads(t *testing.T) {
	withCallbackTokens(t)
	var payload struct {
		TaskID        string `json:"task_id"`
		IssueID       string `json:"issue_id"`
		CallbackToken string `json:"callback_token"`
	}
	var grant service.CallbackGrant
	var resolveErr error
	sentinel := quietServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		// Resolved during the call: the grant is revoked once it returns.
		grant, resolveErr = testHandler.PluginService.Callbacks.Resolve(payload.CallbackToken)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	installationID := installExamplePlugin(t, exampleServers{sentinel: sentinel, metrics: metricsServer(t)})
	run := issueRun(t, "Agent hook run context", 0)

	result, err := testHandler.PluginService.InvokeAgentHook(context.Background(), installationID,
		"correlate_deploys", parseUUID(run.agentID),
		service.AgentHookRun{TaskID: parseUUID(run.taskID), IssueID: parseUUID(run.issueID)},
		json.RawMessage(`{"service":"checkout-api","window_minutes":5}`))
	if err != nil || result.Status != "ok" {
		t.Fatalf("invoke: status=%q err=%v", result.Status, err)
	}
	if payload.TaskID != run.taskID || payload.IssueID != run.issueID {
		t.Fatalf("body task_id=%q issue_id=%q, want the calling run %s on %s", payload.TaskID, payload.IssueID, run.taskID, run.issueID)
	}
	if resolveErr != nil {
		t.Fatalf("resolve callback token during the call: %v", resolveErr)
	}
	if uuidToString(grant.TaskID) != run.taskID {
		t.Fatalf("grant task = %s, want %s", uuidToString(grant.TaskID), run.taskID)
	}
	if grant.IssueID.Valid {
		t.Fatalf("grant issue = %s, want none: agent tools must keep reaching other issues", uuidToString(grant.IssueID))
	}
}

// A caller without a run explicitly passes empty run context; zero ids stay absent.
func TestAgentHookWithoutARunSendsNoRunContext(t *testing.T) {
	var raw map[string]any
	sentinel := quietServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	installationID := installExamplePlugin(t, exampleServers{sentinel: sentinel, metrics: metricsServer(t)})
	if _, err := testHandler.PluginService.InvokeAgentHook(context.Background(), installationID,
		"correlate_deploys", parseUUID(testUserID), service.AgentHookRun{}, json.RawMessage(`{"service":"x","window_minutes":5}`)); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if _, present := raw["task_id"]; present {
		t.Fatalf("task_id present without a run: %v", raw["task_id"])
	}
	if _, present := raw["issue_id"]; present {
		t.Fatalf("issue_id present without a run: %v", raw["issue_id"])
	}
}
