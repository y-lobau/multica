package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const timedFixtureEnv = "MULTICA_TIMED_TEST_FIXTURE"

type timedFixtureStep struct {
	Read   bool
	EOF    bool
	Text   string
	Delay  time.Duration
	Marker string
}

type timedFixture struct {
	Steps       []timedFixtureStep
	ACP         bool
	Auth        bool
	SessionID   string
	Text        string
	Tail        string
	ReadyAfter  time.Duration
	LoadSession bool
	Requests    string
	Args        string
}

// Timing-sensitive protocol tests use an already-running Go child. An external
// shell sleep includes process startup (about 240ms on the Holas Mac), which
// otherwise exceeds the very deadlines these fixtures are meant to exercise.
func writeTimedFixture(t *testing.T, name string, fixture timedFixture) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	plan := filepath.Join(dir, "fixture.json")
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan, data, 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	path := filepath.Join(dir, name)
	writeTestExecutable(t, path, []byte("#!/bin/sh\n"+timedFixtureEnv+"="+quote(plan)+" exec "+quote(self)+" \"$@\"\n"))
	return path
}

// Keep the linear scripts in their tests readable, but execute their small,
// explicitly checked instruction set without spawning a process per sleep.
func writeTimedStreamFixture(t *testing.T, name, body string) string {
	t.Helper()
	var fixture timedFixture
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "", line == "#!/bin/sh":
		case line == "read line":
			fixture.Steps = append(fixture.Steps, timedFixtureStep{Read: true})
		case line == "cat > /dev/null":
			fixture.Steps = append(fixture.Steps, timedFixtureStep{EOF: true})
		case strings.HasPrefix(line, "echo ready > "):
			fixture.Steps = append(fixture.Steps, timedFixtureStep{Marker: strings.TrimPrefix(line, "echo ready > ")})
		case strings.HasPrefix(line, "sleep "):
			seconds, err := strconv.ParseFloat(strings.TrimPrefix(line, "sleep "), 64)
			if err != nil || seconds < 0 {
				t.Fatalf("invalid fixture delay: %q", line)
			}
			fixture.Steps = append(fixture.Steps, timedFixtureStep{Delay: time.Duration(seconds * float64(time.Second))})
		case strings.HasPrefix(line, "echo '"), strings.HasPrefix(line, "printf '%s\\n' '"):
			text := strings.TrimPrefix(strings.TrimPrefix(line, "echo '"), "printf '%s\\n' '")
			if !strings.HasSuffix(text, "'") || !json.Valid([]byte(strings.TrimSuffix(text, "'"))) {
				t.Fatalf("invalid fixture JSON: %q", line)
			}
			fixture.Steps = append(fixture.Steps, timedFixtureStep{Text: strings.TrimSuffix(text, "'")})
		default:
			t.Fatalf("unsupported timed fixture instruction: %q", line)
		}
	}
	return writeTimedFixture(t, name, fixture)
}

func runTimedFixture(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var fixture timedFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		panic(err)
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("codex-cli 0.0.0-test")
		return
	}
	if fixture.ACP {
		runTimedACPFixture(fixture)
		return
	}
	in := bufio.NewReader(os.Stdin)
	for _, step := range fixture.Steps {
		switch {
		case step.Read:
			if _, err := in.ReadString('\n'); err != nil {
				panic(err)
			}
		case step.EOF:
			if _, err := io.Copy(io.Discard, in); err != nil {
				panic(err)
			}
		case step.Delay > 0:
			time.Sleep(step.Delay)
		case step.Marker != "":
			if err := os.WriteFile(step.Marker, []byte("ready\n"), 0o600); err != nil {
				panic(err)
			}
		default:
			fmt.Println(step.Text)
		}
	}
}

func runTimedACPFixture(f timedFixture) {
	if f.Args != "" {
		if err := os.WriteFile(f.Args, []byte(strings.Join(os.Args[1:], " ")+"\n"), 0o600); err != nil {
			panic(err)
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	send := func(v any) {
		if err := encoder.Encode(v); err != nil {
			panic(err)
		}
	}
	chunk := func(text string) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
			"sessionId": f.SessionID, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": text}},
		}})
	}
	var readyAt time.Time
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		if f.Requests != "" {
			file, err := os.OpenFile(f.Requests, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				panic(err)
			}
			_, err = fmt.Fprintln(file, in.Text())
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				panic(fmt.Sprintf("record request: %v %v", err, closeErr))
			}
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(in.Bytes(), &request); err != nil {
			panic(err)
		}
		if len(request.ID) == 0 { // initialized is a notification
			continue
		}
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			readyAt = time.Now().Add(f.ReadyAfter)
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{
				"loadSession": f.LoadSession, "mcpCapabilities": map[string]any{"http": true, "sse": true},
			}}
			if f.Auth {
				result["authMethods"] = []map[string]string{{"id": "cached_token", "name": "Cached login"}}
			}
		case "authenticate":
		case "session/new":
			if time.Now().Before(readyAt) {
				os.Exit(42)
			}
			result["sessionId"] = f.SessionID
		case "session/prompt":
			if f.Tail != "" {
				chunk(f.Text)
			}
			result["stopReason"] = "end_turn"
		case "session/load":
		default:
			send(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
			continue
		}
		send(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		if request.Method == "session/prompt" {
			time.Sleep(50 * time.Millisecond)
			text := f.Text
			if f.Tail != "" {
				text = f.Tail
			}
			chunk(text)
		}
	}
	if err := in.Err(); err != nil {
		panic(err)
	}
}
