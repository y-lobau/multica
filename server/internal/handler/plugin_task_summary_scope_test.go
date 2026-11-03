package handler

import (
	"context"
	"github.com/multica-ai/multica/server/internal/testutil"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
	"net/http"
	"testing"
)

func TestPluginTaskTriggerSummaryRequiresCommentsRead(t *testing.T) {
	installationID := installPluginForAction(t, []string{"tasks:read"})
	runtimeID := dbfx.Runtime(t, "qa-summary-runtime")
	agentID := dbfx.Agent(t, "qa-summary-agent", runtimeID)
	issueID := dbfx.Issue(t, "QA synthetic summary boundary")
	marker := "QA_SYNTHETIC_COMMENT_WITHOUT_COMMENTS_READ"
	commentID := dbfx.Comment(t, issueID, marker)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":           issueID,
		"trigger_comment_id": commentID,
		"trigger_summary":    marker,
		"status":             "completed",
		"runtime_id":         runtimeID,
	})
	request := pluginActionRequest(http.MethodGet, "/v1/tasks/"+taskID, installationID, nil, map[string]string{"task_id": taskID})
	var task publicapiv1.Task
	testutil.Call(t, testHandler.GetPluginTask, request).Want(http.StatusOK).JSON(&task)
	if task.TriggerSummary != nil && *task.TriggerSummary == marker {
		t.Fatalf("comments:read absent, but synthetic comment text returned in trigger_summary: %s", *task.TriggerSummary)
	}
}

func TestPluginTaskSummaryPreservesAuthorizedCommentRead(t *testing.T) {
	installationID := installPluginForAction(t, []string{"tasks:read", "comments:read"})
	run := issueRun(t, "Summary authorized read", 0)
	marker := "AUTHORIZED_SUMMARY_MARKER"
	_, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET trigger_summary = $1 WHERE id = $2`, marker, run.taskID)
	if err != nil {
		t.Fatal(err)
	}
	var task publicapiv1.Task
	testutil.Call(t, testHandler.GetPluginTask, pluginActionRequest(http.MethodGet, "/v1/tasks/"+run.taskID, installationID, nil, map[string]string{"task_id": run.taskID})).Want(http.StatusOK).JSON(&task)
	if task.TriggerSummary == nil || *task.TriggerSummary != marker || len(task.Input) != 1 {
		t.Fatalf("authorized summary/input missing: %+v", task)
	}
}

func TestPluginChatSummaryRequiresChatsRead(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "summary-chat-runtime")
	agentID := dbfx.Agent(t, "summary-chat-agent", runtimeID)
	sessionID := dbfx.ChatSession(t, agentID)
	marker := "PRIVATE_CHAT_SUMMARY_MARKER"
	taskID := dbfx.Task(t, agentID, testutil.Cols{"chat_session_id": sessionID, "runtime_id": runtimeID, "status": "completed", "trigger_summary": marker})
	for _, allowed := range []bool{false, true} {
		scopes := []string{"tasks:read"}
		if allowed {
			scopes = append(scopes, "chats:read")
		}
		installationID := installPluginForAction(t, scopes)
		var task publicapiv1.Task
		testutil.Call(t, testHandler.GetPluginTask, pluginActionRequest(http.MethodGet, "/v1/tasks/"+taskID, installationID, nil, map[string]string{"task_id": taskID})).Want(http.StatusOK).JSON(&task)
		if allowed && (task.TriggerSummary == nil || *task.TriggerSummary != marker) {
			t.Fatal("authorized chat summary missing")
		}
		if !allowed && task.TriggerSummary != nil {
			t.Fatal("chat summary leaked without chats:read")
		}
	}
}
