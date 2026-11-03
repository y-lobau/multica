package publicapiv1

// Context is Plugin-specific bootstrap data. It lives in the versioned
// contract package so service-layer fields cannot leak into the wire format.
type Context struct {
	Workspace         ContextWorkspace `json:"workspace"`
	User              *ContextUser     `json:"user,omitempty"`
	Issue             *ContextIssue    `json:"issue,omitempty"`
	Config            map[string]any   `json:"config"`
	GrantedNetDomains []string         `json:"granted_net_domains"`
	Actor             string           `json:"actor"`
}

type ContextWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type ContextUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ContextIssue struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// Issue intentionally mirrors the fields already exposed by Plugin API v1,
// but is independent from handler.IssueResponse. Adding an App API field can
// therefore no longer widen the public contract by accident.
type Issue struct {
	ID             string         `json:"id"`
	WorkspaceID    string         `json:"workspace_id"`
	Number         int32          `json:"number"`
	Identifier     string         `json:"identifier"`
	Title          string         `json:"title"`
	Description    *string        `json:"description"`
	Status         string         `json:"status"`
	StatusCategory string         `json:"status_category,omitempty"`
	Priority       string         `json:"priority"`
	AssigneeType   *string        `json:"assignee_type"`
	AssigneeID     *string        `json:"assignee_id"`
	CreatorType    string         `json:"creator_type"`
	CreatorID      string         `json:"creator_id"`
	ParentIssueID  *string        `json:"parent_issue_id"`
	ProjectID      *string        `json:"project_id"`
	Position       float64        `json:"position"`
	Stage          *int32         `json:"stage"`
	StartDate      *string        `json:"start_date"`
	DueDate        *string        `json:"due_date"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	Revision       int64          `json:"revision"`
	LastActivityAt *string        `json:"last_activity_at"`
	Metadata       map[string]any `json:"metadata"`
	Properties     map[string]any `json:"properties"`
}

type PatchIssueRequest struct {
	ExpectedRevision *int64  `json:"expected_revision,omitempty"`
	Title            *string `json:"title,omitempty"`
	Description      *string `json:"description,omitempty"`
}

type Comment struct {
	ID         string `json:"id"`
	AuthorType string `json:"author_type"`
	AuthorID   string `json:"author_id"`
	Content    string `json:"content"`
	Type       string `json:"type"`
	ParentID   string `json:"parent_id,omitempty"`
	CreatedAt  string `json:"created_at"`
	// DeletedAt is set only on a tombstone: a comment deleted while it still
	// had replies, kept with empty content so those replies keep their parent.
	DeletedAt string `json:"deleted_at,omitempty"`
}

type CommentListResponse struct {
	Comments []Comment `json:"comments"`
}

type CreateCommentRequest struct {
	Content  string  `json:"content"`
	ParentID *string `json:"parent_id,omitempty"`
}

type StorageKey struct {
	Key       string `json:"key"`
	SizeBytes int64  `json:"size_bytes"`
	UpdatedAt string `json:"updated_at"`
}

type StorageKeyListResponse struct {
	Keys []StorageKey `json:"keys"`
}

type StorageValueResponse struct {
	Value string `json:"value"`
}

type PutStorageValueRequest struct {
	Value string `json:"value"`
}

// Task is one agent run. Kind says what started it, and only the link that
// kind implies is guaranteed: an issue run has issue_id, a chat run has
// chat_session_id and chat_user_id, an autopilot run has autopilot_id.
type Task struct {
	ID                   string      `json:"id"`
	WorkspaceID          string      `json:"workspace_id"`
	AgentID              string      `json:"agent_id"`
	Kind                 string      `json:"kind"`
	Status               string      `json:"status"`
	IssueID              *string     `json:"issue_id"`
	ChatSessionID        *string     `json:"chat_session_id"`
	ChatUserID           *string     `json:"chat_user_id"`
	AutopilotID          *string     `json:"autopilot_id"`
	AutopilotRunID       *string     `json:"autopilot_run_id"`
	OriginatorUserID     *string     `json:"originator_user_id"`
	TriggerCommentID     *string     `json:"trigger_comment_id"`
	TriggerSummary       *string     `json:"trigger_summary"`
	DelegatedFromTaskID  *string     `json:"delegated_from_task_id"`
	DelegatedFromAgentID *string     `json:"delegated_from_agent_id"`
	RetryOfTaskID        *string     `json:"retry_of_task_id"`
	Attempt              int32       `json:"attempt"`
	MaxAttempts          int32       `json:"max_attempts"`
	FailureReason        *string     `json:"failure_reason"`
	Input                []TaskInput `json:"input"`
	CreatedAt            string      `json:"created_at"`
	StartedAt            *string     `json:"started_at"`
	CompletedAt          *string     `json:"completed_at"`
}

// TaskInput is what a person or another agent asked for when the run began:
// the chat message, the triggering comment, the quick-create prompt or the
// delegation handoff note. Sources the caller was not granted are omitted.
type TaskInput struct {
	Source     string `json:"source"`
	AuthorType string `json:"author_type,omitempty"`
	AuthorID   string `json:"author_id,omitempty"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
}

// TaskMessage is one transcript entry, in the run's own seq order.
type TaskMessage struct {
	Seq             int32          `json:"seq"`
	Type            string         `json:"type"`
	Tool            string         `json:"tool,omitempty"`
	CallID          string         `json:"call_id,omitempty"`
	Content         string         `json:"content,omitempty"`
	Input           map[string]any `json:"input,omitempty"`
	Output          string         `json:"output,omitempty"`
	OutputTruncated *bool          `json:"output_truncated,omitempty"`
	Truncated       bool           `json:"truncated,omitempty"`
	CreatedAt       string         `json:"created_at"`
}

type TaskMessageListResponse struct {
	Messages []TaskMessage `json:"messages"`
	PageInfo
}
