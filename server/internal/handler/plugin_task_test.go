package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

// Reading agent runs through the Plugin Action API. As with issues, the
// interesting properties are the refusals: a token about one run must not
// reach another, a token about one issue must not reach another issue's runs,
// and a chat's words stay behind their own consent.

func runCallbackToken(t *testing.T, installationID, taskID, issueID string) string {
	t.Helper()
	installation, err := testHandler.PluginService.InstallationForWorkspace(
		context.Background(), parseUUID(testWorkspaceID), installationID)
	if err != nil {
		t.Fatalf("load installation: %v", err)
	}
	invocation := service.HookInvocation{
		Installation: installation,
		Hook:         plugincontract.Hook{Key: "summarize"},
		Trigger:      plugincontract.TriggerEvent,
		Actor:        service.HookActor{Type: "plugin", ID: parseUUID(installationID)},
	}
	if taskID != "" {
		invocation.TaskID = parseUUID(taskID)
	}
	if issueID != "" {
		invocation.IssueID = parseUUID(issueID)
	}
	token, err := testHandler.PluginService.Callbacks.Issue(context.Background(), invocation)
	if err != nil {
		t.Fatalf("issue callback token: %v", err)
	}
	return token
}

type runFixture struct {
	agentID string
	issueID string
	taskID  string
}

// issueRun creates an agent with one completed run on a fresh issue, started
// by a member comment, with a transcript of n text messages.
func issueRun(t *testing.T, title string, n int) runFixture {
	t.Helper()
	runtimeID := dbfx.Runtime(t, "plugin-task-runtime-"+uuid.NewString()[:8])
	agentID := dbfx.Agent(t, "plugin-task-agent-"+uuid.NewString()[:8], runtimeID)
	issueID := dbfx.Issue(t, title)
	commentID := dbfx.Comment(t, issueID, "please compare Kafka and RocketMQ")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":           issueID,
		"trigger_comment_id": commentID,
		"status":             "completed",
		"runtime_id":         runtimeID,
	})
	for seq := 1; seq <= n; seq++ {
		dbfx.Insert(t, "task_message", testutil.Cols{
			"task_id": taskID, "seq": seq, "type": "text", "content": "step " + string(rune('0'+seq)),
		})
	}
	return runFixture{agentID: agentID, issueID: issueID, taskID: taskID}
}

func getPluginTask(t *testing.T, want int, request *http.Request) (*testutil.Response, publicapiv1.Task) {
	t.Helper()
	recorder := testutil.Call(t, testHandler.GetPluginTask, request).Want(want)
	var task publicapiv1.Task
	if recorder.Code == http.StatusOK {
		recorder.JSON(&task)
	}
	return recorder, task
}

func listPluginTaskMessages(t *testing.T, want int, request *http.Request) (*testutil.Response, publicapiv1.TaskMessageListResponse) {
	t.Helper()
	recorder := testutil.Call(t, testHandler.ListPluginTaskMessages, request).Want(want)
	var page publicapiv1.TaskMessageListResponse
	if recorder.Code == http.StatusOK {
		recorder.JSON(&page)
	}
	return recorder, page
}

func TestPluginTaskRequiresTasksRead(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read"})
	run := issueRun(t, "Plugin task scope", 1)

	recorder, _ := getPluginTask(t, http.StatusForbidden, pluginActionRequest(http.MethodGet, "/v1/tasks/"+run.taskID,
		installationID, nil, map[string]string{"task_id": run.taskID}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403 without tasks:read", recorder.Code, recorder.Body.String())
	}
}

func TestPluginTaskReadsIssueRunInputAndPagedTranscript(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "tasks:read", "comments:read"})
	run := issueRun(t, "Plugin task read", 3)
	params := map[string]string{"task_id": run.taskID}

	recorder, task := getPluginTask(t, http.StatusOK, pluginActionRequest(http.MethodGet, "/v1/tasks/"+run.taskID, installationID, nil, params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if task.Kind != "issue" || task.IssueID == nil || *task.IssueID != run.issueID || task.AgentID != run.agentID {
		t.Fatalf("task = %+v, want an issue run of agent %s on %s", task, run.agentID, run.issueID)
	}
	if len(task.Input) != 1 || task.Input[0].Source != "comment" || task.Input[0].Content != "please compare Kafka and RocketMQ" || task.Input[0].AuthorType != "member" {
		t.Fatalf("input = %+v, want the triggering member comment", task.Input)
	}

	recorder, page := listPluginTaskMessages(t, http.StatusOK, pluginActionRequest(http.MethodGet, "/v1/tasks/"+run.taskID+"/messages?limit=2", installationID, nil, params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(page.Messages) != 2 || page.Messages[0].Seq != 1 || page.NextCursor == "" {
		t.Fatalf("first page = %+v, want seq 1..2 and a next cursor", page)
	}
	cursor := page.NextCursor
	recorder, page = listPluginTaskMessages(t, http.StatusOK, pluginActionRequest(http.MethodGet, "/v1/tasks/"+run.taskID+"/messages?cursor="+cursor, installationID, nil, params))
	if recorder.Code != http.StatusOK || len(page.Messages) != 1 || page.Messages[0].Seq != 3 || page.NextCursor != "" {
		t.Fatalf("second page status=%d page=%+v, want only seq 3 and no next", recorder.Code, page)
	}

	// A cursor belongs to its run: replaying it against another run is refused
	// rather than silently paging someone else's transcript.
	other := issueRun(t, "Plugin task cursor replay", 3)
	recorder, _ = listPluginTaskMessages(t, http.StatusBadRequest, pluginActionRequest(http.MethodGet, "/v1/tasks/"+other.taskID+"/messages?cursor="+cursor, installationID, nil,
		map[string]string{"task_id": other.taskID}))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("replayed cursor status=%d, want 400", recorder.Code)
	}

	recorder, _ = listPluginTaskMessages(t, http.StatusBadRequest, pluginActionRequest(http.MethodGet, "/v1/tasks/"+run.taskID+"/messages?limit=0", installationID, nil, params))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("limit=0 status=%d, want 400", recorder.Code)
	}
}

// Without comments:read the trigger comment is withheld; the run itself is
// still readable, because the comment is issue content with its own scope.
func TestPluginTaskWithholdsCommentInputWithoutCommentsRead(t *testing.T) {
	installationID := installPluginForAction(t, []string{"tasks:read"})
	run := issueRun(t, "Plugin task comment scope", 0)
	recorder, task := getPluginTask(t, http.StatusOK, pluginActionRequest(http.MethodGet, "/v1/tasks/"+run.taskID,
		installationID, nil, map[string]string{"task_id": run.taskID}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(task.Input) != 0 || task.TriggerCommentID == nil {
		t.Fatalf("input=%+v trigger=%v, want the id without the content", task.Input, task.TriggerCommentID)
	}
}

func TestPluginTaskCallbackAboutOneRunCannotReadAnother(t *testing.T) {
	withCallbackTokens(t)
	installationID := installPluginForAction(t, []string{"issues:read", "tasks:read"})
	mine := issueRun(t, "Plugin task narrowed mine", 1)
	other := issueRun(t, "Plugin task narrowed other", 1)
	token := runCallbackToken(t, installationID, mine.taskID, "")

	recorder, _ := getPluginTask(t, http.StatusNotFound, callbackRequest(token, http.MethodGet, "/v1/tasks/"+other.taskID, nil,
		map[string]string{"task_id": other.taskID}))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("other run status=%d, want 404: a token about one run must not reach another", recorder.Code)
	}
	recorder, _ = listPluginTaskMessages(t, http.StatusNotFound, callbackRequest(token, http.MethodGet, "/v1/tasks/"+other.taskID+"/messages", nil,
		map[string]string{"task_id": other.taskID}))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("other transcript status=%d, want 404", recorder.Code)
	}
	recorder, task := getPluginTask(t, http.StatusOK, callbackRequest(token, http.MethodGet, "/v1/tasks/"+mine.taskID, nil,
		map[string]string{"task_id": mine.taskID}))
	if recorder.Code != http.StatusOK || task.ID != mine.taskID {
		t.Fatalf("own run status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginTaskIssueGrantReachesOnlyThatIssuesRuns(t *testing.T) {
	withCallbackTokens(t)
	installationID := installPluginForAction(t, []string{"issues:read", "tasks:read"})
	mine := issueRun(t, "Plugin task issue grant mine", 1)
	other := issueRun(t, "Plugin task issue grant other", 1)
	token := runCallbackToken(t, installationID, "", mine.issueID)

	recorder, _ := getPluginTask(t, http.StatusNotFound, callbackRequest(token, http.MethodGet, "/v1/tasks/"+other.taskID, nil,
		map[string]string{"task_id": other.taskID}))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("run on another issue status=%d, want 404", recorder.Code)
	}
	recorder, _ = getPluginTask(t, http.StatusOK, callbackRequest(token, http.MethodGet, "/v1/tasks/"+mine.taskID, nil,
		map[string]string{"task_id": mine.taskID}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("run on the granted issue status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginChatRunNeedsChatsReadForWhatWasSaid(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "plugin-chat-runtime-"+uuid.NewString()[:8])
	agentID := dbfx.Agent(t, "plugin-chat-agent-"+uuid.NewString()[:8], runtimeID)
	sessionID := dbfx.ChatSession(t, agentID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"chat_session_id": sessionID, "status": "completed", "runtime_id": runtimeID})
	dbfx.Insert(t, "chat_message", testutil.Cols{
		"chat_session_id": sessionID, "role": "user", "content": "remember: comments in Chinese", "task_id": taskID,
	})
	dbfx.Insert(t, "task_message", testutil.Cols{"task_id": taskID, "seq": 1, "type": "text", "content": "noted"})
	params := map[string]string{"task_id": taskID}

	withoutChats := installPluginForAction(t, []string{"tasks:read"})
	recorder, task := getPluginTask(t, http.StatusOK, pluginActionRequest(http.MethodGet, "/v1/tasks/"+taskID, withoutChats, nil, params))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if task.Kind != "chat" || task.ChatUserID == nil || *task.ChatUserID != testUserID {
		t.Fatalf("task = %+v, want a chat run of %s", task, testUserID)
	}
	if len(task.Input) != 0 {
		t.Fatalf("input = %+v, want the chat message withheld without chats:read", task.Input)
	}
	recorder, _ = listPluginTaskMessages(t, http.StatusForbidden, pluginActionRequest(http.MethodGet, "/v1/tasks/"+taskID+"/messages", withoutChats, nil, params))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("chat transcript status=%d, want 403 without chats:read", recorder.Code)
	}

	withChats := installPluginForAction(t, []string{"tasks:read", "chats:read"})
	recorder, task = getPluginTask(t, http.StatusOK, pluginActionRequest(http.MethodGet, "/v1/tasks/"+taskID, withChats, nil, params))
	if recorder.Code != http.StatusOK || len(task.Input) != 1 || task.Input[0].Source != "chat_message" || task.Input[0].Content != "remember: comments in Chinese" {
		t.Fatalf("status=%d input=%+v, want the member's chat message", recorder.Code, task.Input)
	}
	recorder, page := listPluginTaskMessages(t, http.StatusOK, pluginActionRequest(http.MethodGet, "/v1/tasks/"+taskID+"/messages", withChats, nil, params))
	if recorder.Code != http.StatusOK || len(page.Messages) != 1 {
		t.Fatalf("status=%d page=%+v", recorder.Code, page)
	}
}

func TestPluginTaskUnknownOrUnresolvableIsNotFound(t *testing.T) {
	installationID := installPluginForAction(t, []string{"tasks:read"})
	runtimeID := dbfx.Runtime(t, "plugin-orphan-runtime-"+uuid.NewString()[:8])
	agentID := dbfx.Agent(t, "plugin-orphan-agent-"+uuid.NewString()[:8], runtimeID)
	// No issue, chat, autopilot or quick-create context: no workspace to match.
	orphan := dbfx.Task(t, agentID, testutil.Cols{"status": "completed", "runtime_id": runtimeID})
	for _, id := range []string{uuid.NewString(), orphan, "not-a-uuid"} {
		recorder, _ := getPluginTask(t, http.StatusNotFound, pluginActionRequest(http.MethodGet, "/v1/tasks/"+id, installationID, nil, map[string]string{"task_id": id}))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("task %s status=%d, want 404", id, recorder.Code)
		}
	}
}

func TestPluginTaskQuickCreateKeepsOriginAfterCreatingIssue(t *testing.T) {
	run := issueRun(t, "quick-create linked issue", 0)
	quick, err := json.Marshal(service.QuickCreateContext{
		Type: service.QuickCreateContextType, RequesterID: testUserID,
		Prompt: "Create a release review with a seven-day window",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET context = $1 WHERE id = $2`, quick, run.taskID); err != nil {
		t.Fatal(err)
	}
	installationID := installPluginForAction(t, []string{"tasks:read"})
	recorder, task := getPluginTask(t, http.StatusOK, pluginActionRequest(http.MethodGet, "/v1/tasks/"+run.taskID, installationID, nil, map[string]string{"task_id": run.taskID}))
	if recorder.Code != http.StatusOK || task.Kind != "quick_create" || task.IssueID == nil || *task.IssueID != run.issueID {
		t.Fatalf("status=%d task=%+v, want the original quick-create kind with its linked issue", recorder.Code, task)
	}
	if len(task.Input) != 1 || task.Input[0].Source != "quick_create" || task.Input[0].Content != "Create a release review with a seven-day window" {
		t.Fatalf("input=%+v, want the member's original quick-create prompt", task.Input)
	}
}
