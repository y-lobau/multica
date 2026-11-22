package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCommentAttachmentPublicationRecovery(t *testing.T) {
	const issueID = "11111111-1111-4111-8111-111111111111"
	const parentID = "22222222-2222-4222-8222-222222222222"
	const body = "Evidence: Belarusian прывітанне\nLiteral \\n stays literal."
	for _, tc := range []struct {
		name       string
		status     int
		failUpload bool
	}{
		{"deleted upload", http.StatusConflict, false},
		{"expired credential", http.StatusUnauthorized, false},
		{"permission denied", http.StatusForbidden, false},
		{"lost response", 0, false},
		{"second upload rejected", http.StatusUnauthorized, true},
		{"published", http.StatusCreated, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			originals := map[string][]byte{"first.txt": []byte("first original\x00bytes"), "second.txt": []byte("second original")}
			for name, data := range originals {
				if err := os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var uploads, posts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && r.URL.Path == "/api/issues/"+issueID:
					json.NewEncoder(w).Encode(map[string]any{"id": issueID, "identifier": "TST-1"})
				case r.Method == "POST" && r.URL.Path == "/api/upload-file":
					upload := uploads.Add(1)
					if tc.failUpload && upload == 2 {
						w.WriteHeader(http.StatusUnauthorized)
						json.NewEncoder(w).Encode(map[string]any{"error": "credential expired"})
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"id": fmt.Sprintf("upload-%d", upload)})
				case r.Method == "POST" && r.URL.Path == "/api/issues/"+issueID+"/comments":
					posts.Add(1)
					var submitted map[string]any
					if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
						t.Error(err)
					}
					if submitted["content"] != body || submitted["parent_id"] != parentID {
						t.Error("body or reply parent changed")
					}
					if tc.status == 0 {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						conn.Close()
						return
					}
					w.WriteHeader(tc.status)
					if tc.status == http.StatusCreated {
						json.NewEncoder(w).Encode(map[string]any{"id": "comment-id", "content": body})
					} else {
						json.NewEncoder(w).Encode(map[string]any{"error": "attachment is no longer available or credential expired"})
					}
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()
			setCLITestServerEnv(t, srv.URL)
			t.Setenv("MULTICA_TOKEN", "mat_SYNTHETIC_ONLY")
			cmd := newIssueCommentAddTestCmd()
			for name, value := range map[string]string{"content-stdin": "true", "parent": parentID} {
				if err := cmd.Flags().Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"first.txt", "second.txt"} {
				if err := cmd.Flags().Set("attachment", name); err != nil {
					t.Fatal(err)
				}
			}
			stdin, err := os.CreateTemp(t.TempDir(), "stdin")
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdin.WriteString(body)
			stdin.Seek(0, io.SeekStart)
			oldIn, oldOut := os.Stdin, os.Stdout
			out, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			os.Stdin, os.Stdout = stdin, out
			t.Cleanup(func() { os.Stdin, os.Stdout = oldIn, oldOut })
			stderr := captureStderr(t)
			runErr := runIssueCommentAdd(cmd, []string{issueID})
			os.Stdin, os.Stdout = oldIn, oldOut
			diagnostic := stderr.read()
			out.Seek(0, io.SeekStart)
			stdout, _ := io.ReadAll(out)
			dirs, err := filepath.Glob(".multica-comment-recovery-*")
			if err != nil {
				t.Fatal(err)
			}
			if tc.status == http.StatusCreated {
				if runErr != nil || posts.Load() != 1 || !strings.Contains(diagnostic, "Comment added") || len(dirs) != 0 {
					t.Fatalf("publication result: err=%v posts=%d diagnostic=%s retained=%v", runErr, posts.Load(), diagnostic, dirs)
				}
				return
			}
			if runErr == nil || len(stdout) != 0 || strings.Contains(diagnostic, "Comment added") {
				t.Fatalf("rejection claimed publication: err=%v stdout=%s diagnostic=%s", runErr, stdout, diagnostic)
			}
			if tc.failUpload && posts.Load() != 0 {
				t.Fatal("posted after failed upload")
			}
			if !tc.failUpload && posts.Load() != 1 {
				t.Fatal("did not attempt publication exactly once")
			}
			if len(dirs) != 1 {
				t.Fatalf("stdin body and files were not retained: %v", dirs)
			}
			dir := dirs[0]
			if !strings.Contains(diagnostic, dir) || !strings.Contains(diagnostic, "not confirmed") {
				t.Fatalf("missing recovery guidance: %s", diagnostic)
			}
			recovered, err := os.ReadFile(filepath.Join(dir, "body.md"))
			if err != nil || string(recovered) != body {
				t.Fatalf("lost authored body: %v", err)
			}
			// Preserve file bytes even if the original is removed after rejection.
			for name, want := range originals {
				got, err := os.ReadFile(name)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("changed original %s", name)
				}
				os.Remove(name)
				copies, err := filepath.Glob(filepath.Join(dir, "attachments", "*-"+name))
				if err != nil || len(copies) != 1 {
					t.Fatalf("lost recoverable file %s", name)
				}
				got, err = os.ReadFile(copies[0])
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("changed saved file %s", name)
				}
			}
			var receipt struct {
				IssueID       string   `json:"issue_id"`
				ParentID      string   `json:"parent_id"`
				AttachmentIDs []string `json:"attachment_ids"`
			}
			data, err := os.ReadFile(filepath.Join(dir, "receipt.json"))
			if err != nil || json.Unmarshal(data, &receipt) != nil {
				t.Fatalf("no publication receipt: %v", err)
			}
			wantIDs := 2
			if tc.failUpload {
				wantIDs = 1
			}
			if receipt.IssueID != issueID || receipt.ParentID != parentID || len(receipt.AttachmentIDs) != wantIDs {
				t.Fatalf("recovery lost target or uploaded receipts: %+v", receipt)
			}
			filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					t.Fatal(err)
				}
				info, err := entry.Info()
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
					t.Errorf("recovery readable outside owner: %s", path)
				}
				if !entry.IsDir() {
					data, _ := os.ReadFile(path)
					if bytes.Contains(data, []byte("mat_SYNTHETIC_ONLY")) {
						t.Error("recovery leaked credential")
					}
				}
				return nil
			})
		})
	}
}
