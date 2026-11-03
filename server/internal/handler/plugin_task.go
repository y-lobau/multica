package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

// Read access to agent runs through the Plugin Action API.
//
// Reach follows the same three checks as issues. The installation must hold
// tasks:read; the task must be in the installation's workspace; and a callback
// token issued about one run or one issue reaches only that run or that
// issue's runs. Chat runs carry a private conversation, so their input and
// transcript additionally need chats:read.

// Cut per text field, not per response: one tool output can be megabytes, and
// a plugin distilling a run has no use for all of it.
const pluginTaskFieldMaxBytes = 32 << 10

// GetPluginTask — GET /v1/tasks/{task_id}
func (h *Handler) GetPluginTask(w http.ResponseWriter, r *http.Request) {
	caller, _, ok := h.pluginCaller(w, r, plugincontract.ScopeTasksRead)
	if !ok {
		return
	}
	task, ok := h.pluginTaskForCaller(w, r, caller, chi.URLParam(r, "task_id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.pluginTaskPayload(r, caller, task))
}

// ListPluginTaskMessages — GET /v1/tasks/{task_id}/messages
func (h *Handler) ListPluginTaskMessages(w http.ResponseWriter, r *http.Request) {
	caller, _, ok := h.pluginCaller(w, r, plugincontract.ScopeTasksRead)
	if !ok {
		return
	}
	task, ok := h.pluginTaskForCaller(w, r, caller, chi.URLParam(r, "task_id"))
	if !ok {
		return
	}
	if task.ChatSessionID.Valid && !slices.Contains(caller.Scopes, plugincontract.ScopeChatsRead) {
		publicapiv1.WriteProblem(w, r, http.StatusForbidden, "forbidden",
			"this run is a chat; reading its transcript requires the "+plugincontract.ScopeChatsRead+" scope")
		return
	}

	afterSeq, ok := taskMessagesCursor(w, r, task.ID)
	if !ok {
		return
	}
	limit, ok := queryInt32(w, r, "limit", publicapiv1.DefaultPageSize, 1, publicapiv1.MaxPageSize)
	if !ok {
		return
	}
	// One extra row says whether another page follows without a count query.
	rows, err := h.Queries.ListTaskMessagesPage(r.Context(), db.ListTaskMessagesPageParams{
		TaskID:    task.ID,
		AfterSeq:  afterSeq,
		PageLimit: limit + 1,
	})
	if err != nil {
		publicapiv1.WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "failed to list task messages")
		return
	}
	response := publicapiv1.TaskMessageListResponse{Messages: make([]publicapiv1.TaskMessage, 0, len(rows))}
	if len(rows) > int(limit) {
		rows = rows[:limit]
		response.NextCursor = encodeTaskMessagesCursor(task.ID, rows[len(rows)-1].Seq)
	}
	for _, row := range rows {
		response.Messages = append(response.Messages, publicTaskMessage(row))
	}
	writeJSON(w, http.StatusOK, response)
}

// pluginTaskForCaller resolves a task the caller may read, or writes the
// problem and reports false. Every refusal is a 404 for the same reason it is
// on issues: a plugin must not be able to confirm that a run it cannot read
// exists.
func (h *Handler) pluginTaskForCaller(w http.ResponseWriter, r *http.Request, caller service.PluginActionCaller, taskRef string) (db.AgentTaskQueue, bool) {
	notFound := func() (db.AgentTaskQueue, bool) {
		publicapiv1.WriteProblem(w, r, http.StatusNotFound, "not_found", "task not found")
		return db.AgentTaskQueue{}, false
	}
	taskID, err := util.ParseUUID(taskRef)
	if err != nil {
		return notFound()
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil {
		if isNotFound(err) {
			return notFound()
		}
		publicapiv1.WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "failed to load task")
		return db.AgentTaskQueue{}, false
	}
	workspaceID, err := h.TaskService.ResolveTaskWorkspaceIDChecked(r.Context(), task)
	if err != nil {
		publicapiv1.WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "failed to load task")
		return db.AgentTaskQueue{}, false
	}
	if workspaceID == "" || workspaceID != uuidToString(caller.WorkspaceID) {
		return notFound()
	}
	// A grant about one run reaches that run, and a grant about one issue
	// reaches that issue's runs. Both narrow; neither widens.
	if caller.TaskScope.Valid && uuidToString(task.ID) != uuidToString(caller.TaskScope) {
		return notFound()
	}
	if caller.IssueScope.Valid && uuidToString(task.IssueID) != uuidToString(caller.IssueScope) {
		return notFound()
	}
	return task, true
}

func (h *Handler) pluginTaskPayload(r *http.Request, caller service.PluginActionCaller, task db.AgentTaskQueue) publicapiv1.Task {
	ctx := r.Context()
	out := publicapiv1.Task{
		ID:                  uuidToString(task.ID),
		WorkspaceID:         uuidToString(caller.WorkspaceID),
		AgentID:             uuidToString(task.AgentID),
		Kind:                pluginTaskKind(task),
		Status:              task.Status,
		IssueID:             uuidToPtr(task.IssueID),
		ChatSessionID:       uuidToPtr(task.ChatSessionID),
		AutopilotRunID:      uuidToPtr(task.AutopilotRunID),
		OriginatorUserID:    uuidToPtr(task.OriginatorUserID),
		TriggerCommentID:    uuidToPtr(task.TriggerCommentID),
		DelegatedFromTaskID: uuidToPtr(task.DelegatedFromTaskID),
		RetryOfTaskID:       uuidToPtr(task.RetryOfTaskID),
		Attempt:             task.Attempt,
		MaxAttempts:         task.MaxAttempts,
		FailureReason:       textToPtr(task.FailureReason),
		Input:               []publicapiv1.TaskInput{},
		CreatedAt:           timestampToString(task.CreatedAt),
		StartedAt:           timestampToPtr(task.StartedAt),
		CompletedAt:         timestampToPtr(task.CompletedAt),
	}
	canReadChats := slices.Contains(caller.Scopes, plugincontract.ScopeChatsRead)
	canReadComments := slices.Contains(caller.Scopes, plugincontract.ScopeCommentsRead)
	// Summaries can contain the same private words as the source input.
	// Comment summaries are released only after validating the comment below.
	if !task.TriggerCommentID.Valid && (!task.ChatSessionID.Valid || canReadChats) {
		out.TriggerSummary = textToPtr(task.TriggerSummary)
	}

	if task.ChatSessionID.Valid {
		if session, err := h.Queries.GetChatSession(ctx, task.ChatSessionID); err == nil {
			out.ChatUserID = uuidToPtr(session.CreatorID)
			if canReadChats {
				if messages, err := h.Queries.ListUserChatMessagesByTask(ctx, task.ID); err == nil {
					for _, message := range messages {
						content, cut := capPluginText(message.Content)
						out.Input = append(out.Input, publicapiv1.TaskInput{
							Source: "chat_message", AuthorType: "member", AuthorID: uuidToString(session.CreatorID),
							Content: content, Truncated: cut, CreatedAt: timestampToString(message.CreatedAt),
						})
					}
				}
			}
		}
	}
	if task.AutopilotRunID.Valid {
		if run, err := h.Queries.GetAutopilotRun(ctx, task.AutopilotRunID); err == nil {
			out.AutopilotID = uuidToPtr(run.AutopilotID)
		}
	}
	if task.TriggerCommentID.Valid && canReadComments {
		// Only a comment on the run's own issue: the trigger id is a row id,
		// and the issue grant is what makes reading it legitimate.
		if comment, err := h.Queries.GetComment(ctx, task.TriggerCommentID); err == nil &&
			uuidToString(comment.IssueID) == uuidToString(task.IssueID) && !comment.DeletedAt.Valid {
			if !task.ChatSessionID.Valid || canReadChats {
				out.TriggerSummary = textToPtr(task.TriggerSummary)
			}
			content, cut := capPluginText(comment.Content)
			out.Input = append(out.Input, publicapiv1.TaskInput{
				Source: "comment", AuthorType: comment.AuthorType, AuthorID: uuidToString(comment.AuthorID),
				Content: content, Truncated: cut, CreatedAt: timestampToString(comment.CreatedAt),
			})
		}
	}
	if task.DelegatedFromTaskID.Valid {
		if parent, err := h.Queries.GetAgentTask(ctx, task.DelegatedFromTaskID); err == nil {
			out.DelegatedFromAgentID = uuidToPtr(parent.AgentID)
		}
	}
	if task.HandoffNote.Valid && task.HandoffNote.String != "" {
		content, cut := capPluginText(task.HandoffNote.String)
		input := publicapiv1.TaskInput{Source: "handoff", Content: content, Truncated: cut}
		if out.DelegatedFromAgentID != nil {
			input.AuthorType, input.AuthorID = "agent", *out.DelegatedFromAgentID
		}
		out.Input = append(out.Input, input)
	}
	if out.Kind == "quick_create" {
		var quick service.QuickCreateContext
		if err := json.Unmarshal(task.Context, &quick); err == nil && quick.Prompt != "" {
			content, cut := capPluginText(quick.Prompt)
			out.Input = append(out.Input, publicapiv1.TaskInput{
				Source: "quick_create", AuthorType: "member", AuthorID: quick.RequesterID, Content: content, Truncated: cut,
			})
		}
	}
	return out
}

// pluginTaskKind names what started a run. A chat run is a chat even if the
// agent went on to file an issue. Quick-create also retains its starting kind
// after linking the created issue. An autopilot run that works an issue is
// reported as an issue run: the issue is where its record belongs.
func pluginTaskKind(task db.AgentTaskQueue) string {
	if task.ChatSessionID.Valid {
		return "chat"
	}
	if len(task.Context) > 0 {
		var quick service.QuickCreateContext
		if err := json.Unmarshal(task.Context, &quick); err == nil && quick.Type == service.QuickCreateContextType {
			return "quick_create"
		}
	}
	switch {
	case task.IssueID.Valid:
		return "issue"
	case task.AutopilotRunID.Valid:
		return "autopilot"
	}
	return "other"
}

func publicTaskMessage(row db.TaskMessage) publicapiv1.TaskMessage {
	out := publicapiv1.TaskMessage{
		Seq:       row.Seq,
		Type:      row.Type,
		Tool:      row.Tool.String,
		CallID:    row.CallID.String,
		CreatedAt: timestampToString(row.CreatedAt),
	}
	var cut bool
	out.Content, cut = capPluginText(row.Content.String)
	out.Truncated = cut
	out.Output, cut = capPluginText(row.Output.String)
	out.Truncated = out.Truncated || cut
	if len(row.Input) > 0 {
		var input map[string]any
		if err := json.Unmarshal(row.Input, &input); err == nil {
			out.Input = input
		}
	}
	if row.OutputTruncated.Valid {
		value := row.OutputTruncated.Bool
		out.OutputTruncated = &value
	}
	return out
}

// capPluginText cuts at a rune boundary so a multi-byte character is never
// split into invalid UTF-8.
func capPluginText(value string) (string, bool) {
	if len(value) <= pluginTaskFieldMaxBytes {
		return value, false
	}
	cut := pluginTaskFieldMaxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut], true
}

// Transcript cursors are opaque to callers but bound to one run, so a cursor
// taken from one run's pages cannot be replayed against another's.
const taskMessagesCursorPrefix = "task_message:"

func encodeTaskMessagesCursor(taskID pgtype.UUID, seq int32) string {
	raw := taskMessagesCursorPrefix + uuidToString(taskID) + ":" + strconv.FormatInt(int64(seq), 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func taskMessagesCursor(w http.ResponseWriter, r *http.Request, taskID pgtype.UUID) (int32, bool) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return 0, true
	}
	invalid := func() (int32, bool) {
		publicapiv1.WriteProblem(w, r, http.StatusBadRequest, "invalid_request", "cursor is not valid for this run")
		return 0, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return invalid()
	}
	prefix := taskMessagesCursorPrefix + uuidToString(taskID) + ":"
	value, found := strings.CutPrefix(string(decoded), prefix)
	if !found {
		return invalid()
	}
	seq, err := strconv.ParseInt(value, 10, 32)
	if err != nil || seq < 0 {
		return invalid()
	}
	return int32(seq), true
}

func queryInt32(w http.ResponseWriter, r *http.Request, name string, fallback, min, max int32) (int32, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, true
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || int32(parsed) < min || int32(parsed) > max {
		publicapiv1.WriteProblem(w, r, http.StatusBadRequest, "invalid_request",
			name+" must be an integer between "+strconv.Itoa(int(min))+" and "+strconv.Itoa(int(max)))
		return 0, false
	}
	return int32(parsed), true
}
