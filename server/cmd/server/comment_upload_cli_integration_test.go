package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	var agentID string
	if err := testPool.QueryRow(context.Background(), `SELECT id::text FROM agent WHERE workspace_id=$1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	taskID := ensureAgentTask(t, agentID)
	token := mintAgentTaskToken(t, agentID, taskID, testUserID)
	bindIssue := func(t *testing.T, issueID string) {
		t.Helper()
		if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET issue_id=$1, status='running', started_at=now(), originator_user_id=$2, accountable_user_id=$2 WHERE id=$3`, issueID, testUserID, taskID); err != nil {
			t.Fatal(err)
		}
	}
	work := t.TempDir()
	childEnv := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + work, "TMPDIR=" + work,
		"MULTICA_SERVER_URL=" + testServer.URL, "MULTICA_TOKEN=" + token, "MULTICA_WORKSPACE_ID=" + testWorkspaceID,
		"MULTICA_TASK_CONFIG_ROOT=" + filepath.Join(work, "config")}
	call := func(args ...string) ([]byte, error) {
		cmd := exec.Command(cli, args...)
		cmd.Dir = work
		cmd.Env = childEnv
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
		t.Run(tc.name, func(t *testing.T) {
			issueID := createIssue(t, "CLI attachment "+tc.name)
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
	issueID := createIssue(t, "CLI description route")
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
}
