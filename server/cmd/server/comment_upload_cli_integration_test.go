package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Explicit opt-in: default tests never discover or execute an installed CLI.
// Set MULTICA_TEST_ATTACHMENT_CLI to the candidate client's absolute path.
func TestCommentUploadCLIAdoption(t *testing.T) {
	cli := os.Getenv("MULTICA_TEST_ATTACHMENT_CLI")
	if cli == "" {
		t.Skip("set MULTICA_TEST_ATTACHMENT_CLI for the CLI/API contract check")
	}
	if testPool == nil {
		t.Fatal("CLI/API contract check requires the migrated disposable database")
	}
	if !filepath.IsAbs(cli) {
		t.Fatal("MULTICA_TEST_ATTACHMENT_CLI must be absolute")
	}
	base := testutil.New(testPool, testWorkspaceID, testUserID)
	workspaceID := base.Workspace(t, "CLI attachment contract", fmt.Sprintf("cli-upload-%d", time.Now().UnixNano()), testutil.Cols{"issue_prefix": "CLI"})
	base.Member(t, workspaceID, testUserID, "owner")
	fixtures := testutil.New(testPool, workspaceID, testUserID)
	runtimeID := fixtures.Runtime(t, "CLI attachment runtime")
	agentID := fixtures.Agent(t, "CLI attachment agent", runtimeID)
	taskID := fixtures.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "originator_user_id": testUserID, "accountable_user_id": testUserID})
	token, err := auth.GenerateAgentTaskToken()
	if err != nil {
		t.Fatal(err)
	}
	fixtures.Insert(t, "task_token", testutil.Cols{"token_hash": auth.HashToken(token), "task_id": taskID, "agent_id": agentID, "workspace_id": workspaceID, "user_id": testUserID, "expires_at": time.Now().Add(time.Hour)})
	bindIssue := func(t *testing.T, issueID string) {
		t.Helper()
		if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET issue_id=$1, status='running', started_at=now(), originator_user_id=$2, accountable_user_id=$2 WHERE id=$3`, issueID, testUserID, taskID); err != nil {
			t.Fatal(err)
		}
	}
	work := t.TempDir()
	childEnv := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + work, "TMPDIR=" + work,
		"MULTICA_TOKEN=" + token, "MULTICA_WORKSPACE_ID=" + workspaceID,
		"MULTICA_TASK_CONFIG_ROOT=" + filepath.Join(work, "config")}
	endpoint := testServer.URL
	call := func(args ...string) ([]byte, error) {
		cmd := exec.Command(cli, args...)
		cmd.Dir = work
		cmd.Env = append(append([]string(nil), childEnv...), "MULTICA_SERVER_URL="+endpoint)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return out, fmt.Errorf("CLI failed: %w: %s", err, stderr.String())
		}
		return out, nil
	}
	jsonCall := func(t *testing.T, out any, args ...string) {
		t.Helper()
		data, err := call(args...)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("decode CLI response: %v: %s", err, data)
		}
	}
	files := map[string][]byte{"report.md": []byte("# Synthetic report\n"), "details.json": []byte("{\"synthetic\":true}\n")}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aWQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	files["shot.png"] = png
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(work, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	type attachment struct {
		ID       string `json:"id"`
		Filename string `json:"filename"`
	}
	type comment struct {
		ID          string       `json:"id"`
		Content     string       `json:"content"`
		AuthorType  string       `json:"author_type"`
		Attachments []attachment `json:"attachments"`
	}
	verifyDownload := func(t *testing.T, a attachment) {
		t.Helper()
		dir := filepath.Join(work, "download-"+a.ID)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Path string `json:"path"`
		}
		jsonCall(t, &result, "attachment", "download", a.ID, "-o", dir)
		got, err := os.ReadFile(result.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, files[a.Filename]) {
			t.Fatalf("download differs: %s", a.Filename)
		}
	}
	for _, tc := range []struct {
		name, body string
		files      []string
	}{
		{"single", "Synthetic report", []string{"report.md"}},
		{"batch", "Synthetic batch\n\n![Screenshot](./shot.png)\n\n!file[Details](./details.json)", []string{"report.md", "shot.png", "details.json"}},
	} {
		issueID := fixtures.Issue(t, "CLI attachment "+tc.name)
		t.Run(tc.name, func(t *testing.T) {
			bindIssue(t, issueID)
			if err := os.WriteFile(filepath.Join(work, "body.md"), []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"issue", "comment", "add", issueID, "--content-file", "body.md", "--output", "json"}
			for _, name := range tc.files {
				args = append(args, "--attachment", name)
			}
			var posted comment
			jsonCall(t, &posted, args...)
			if posted.AuthorType != "agent" || len(posted.Attachments) != len(tc.files) {
				t.Fatalf("wrong author or attachment count: %+v", posted)
			}
			var readback []comment
			jsonCall(t, &readback, "issue", "comment", "list", issueID, "--thread", posted.ID, "--tail", "1", "--output", "json")
			if len(readback) != 1 || len(readback[0].Attachments) != len(tc.files) {
				t.Fatalf("readback lost attachments: %+v", readback)
			}
			for _, a := range readback[0].Attachments {
				verifyDownload(t, a)
			}
			if tc.name == "batch" && (!strings.Contains(posted.Content, "![Screenshot](/api/attachments/") || !strings.Contains(posted.Content, "!file[Details](/api/attachments/")) {
				t.Fatalf("image/file references not rewritten: %s", posted.Content)
			}
			before := len(readback)
			if _, err := call("issue", "comment", "add", issueID, "--content", "missing-file probe", "--attachment", "absent.txt"); err == nil {
				t.Fatal("missing file was accepted")
			}
			var all []comment
			jsonCall(t, &all, "issue", "comment", "list", issueID, "--roots-only", "--output", "json")
			if len(all) != before {
				t.Fatal("missing file created a comment")
			}
		})
	}
	issueID := fixtures.Issue(t, "CLI description route")
	bindIssue(t, issueID)
	var updated map[string]any
	jsonCall(t, &updated, "issue", "update", issueID, "--attachment", "report.md", "--output", "json")
	jsonCall(t, &updated, "issue", "get", issueID, "--output", "json")
	if !strings.Contains(fmt.Sprint(updated["description"]), "report.md") {
		t.Fatal("description route lost attachment reference")
	}
	var id string
	if err := testPool.QueryRow(context.Background(), `SELECT id::text FROM attachment WHERE issue_id=$1 AND comment_id IS NULL`, issueID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	verifyDownload(t, attachment{ID: id, Filename: "report.md"})
	// Inject failures after real task-authenticated uploads, at the publication
	// boundary. The candidate CLI must preserve bytes without reporting success.
	for _, kind := range []string{"deleted upload", "expired task token"} {
		issueID := fixtures.Issue(t, "CLI recovery "+kind)
		t.Run(kind, func(t *testing.T) {
			bindIssue(t, issueID)
			const body = "Synthetic recovery report: прывітанне"
			if err := os.WriteFile(filepath.Join(work, "body.md"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			var posts atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == "/api/issues/"+issueID+"/comments" {
					posts.Add(1)
					payload, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(payload))
					var submitted struct {
						IDs []string `json:"attachment_ids"`
					}
					if err := json.Unmarshal(payload, &submitted); err != nil || len(submitted.IDs) != 1 {
						t.Errorf("missing upload receipt: %v", err)
						w.WriteHeader(500)
						return
					}
					if kind == "deleted upload" {
						_, err = testPool.Exec(context.Background(), "DELETE FROM attachment WHERE id=$1", submitted.IDs[0])
					} else {
						_, err = testPool.Exec(context.Background(), "UPDATE task_token SET expires_at=now()-interval '1 hour' WHERE token_hash=$1", auth.HashToken(token))
					}
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					rec := httptest.NewRecorder()
					testServer.Config.Handler.ServeHTTP(rec, r)
					wantStatus := http.StatusConflict
					if kind == "expired task token" {
						wantStatus = http.StatusUnauthorized
					}
					if rec.Code != wantStatus {
						t.Errorf("publication status=%d, want %d", rec.Code, wantStatus)
					}
					for key, values := range rec.Header() {
						for _, value := range values {
							w.Header().Add(key, value)
						}
					}
					w.WriteHeader(rec.Code)
					w.Write(rec.Body.Bytes())
					return
				}
				testServer.Config.Handler.ServeHTTP(w, r)
			}))
			defer proxy.Close()
			previous := endpoint
			endpoint = proxy.URL
			defer func() { endpoint = previous }()
			defer func() {
				if _, err := testPool.Exec(context.Background(), "UPDATE task_token SET expires_at=now()+interval '1 hour' WHERE token_hash=$1", auth.HashToken(token)); err != nil {
					t.Error(err)
				}
			}()
			out, err := call("issue", "comment", "add", issueID, "--content-file", "body.md", "--attachment", "report.md", "--output", "json")
			if err == nil || len(out) != 0 || posts.Load() != 1 || !strings.Contains(err.Error(), "publication not confirmed") || strings.Contains(err.Error(), "Comment added") {
				t.Fatalf("wrong publication result: err=%v stdout=%q posts=%d", err, out, posts.Load())
			}
			var count int
			if err := testPool.QueryRow(context.Background(), "SELECT count(*) FROM comment WHERE issue_id=$1", issueID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected publication left a comment: count=%d err=%v", count, err)
			}
			dirs, err := filepath.Glob(filepath.Join(work, ".multica-comment-recovery-*"))
			if err != nil || len(dirs) != 1 {
				t.Fatalf("missing recovery: %v %v", dirs, err)
			}
			recovered, err := os.ReadFile(filepath.Join(dirs[0], "body.md"))
			if err != nil || string(recovered) != body {
				t.Fatalf("lost body: %v", err)
			}
			recovered, err = os.ReadFile(filepath.Join(dirs[0], "attachments", "001-report.md"))
			if err != nil || !bytes.Equal(recovered, files["report.md"]) {
				t.Fatalf("lost file bytes: %v", err)
			}
			if err := os.RemoveAll(dirs[0]); err != nil {
				t.Fatal(err)
			}
			t.Log("real task-token upload accepted; publication rejected; no comment; original body and file bytes retained")
		})
	}
}
