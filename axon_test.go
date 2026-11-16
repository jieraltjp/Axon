package axon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type item struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Tags  []string `json:"tags,omitempty"`
	Count int      `json:"count"`
}

type findInput struct {
	Query string   `arg:"" help:"what to find"`
	Limit int      `default:"10" help:"max results"`
	Sort  string   `enum:"name,count" help:"sort key"`
	Tag   []string `short:"t"`
	Token string   `env:"TEST_TOKEN"`
	Exact bool
}

type harness struct {
	app      *App
	stdin    *strings.Reader
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	deleted  []string
	lastFind findInput
}

// newHarness builds a test app. tty controls whether stdin/stdout look like a terminal.
func newHarness(t *testing.T, tty bool, stdin string) *harness {
	t.Helper()
	h := &harness{stdin: strings.NewReader(stdin), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	app := New("tool", Version("1.2.3"), Description("A test tool"))
	app.stdin, app.stdout, app.stderr = h.stdin, h.stdout, h.stderr
	app.stdinTTY, app.stdoutTTY = tty, tty
	t.Setenv("AXON_OUTPUT", "")

	Command(app, "find", "Find items",
		func(ctx *Context, in findInput) ([]item, error) {
			h.lastFind = in
			return []item{{ID: "1", Name: "alpha", Tags: []string{"a", "b"}, Count: 3}, {ID: "2", Name: "beta", Count: 5}}, nil
		},
		Example("tool find alpha"), Aliases("f"))
	Command(app, "get", "Get an item",
		func(ctx *Context, in struct {
			ID string `arg:""`
		}) (item, error) {
			if in.ID != "1" {
				return item{}, NotFound("ITEM_NOT_FOUND", "item %s not found", in.ID).WithHint("run 'tool find'")
			}
			return item{ID: "1", Name: "alpha", Count: 3}, nil
		})
	Command(app, "delete", "Delete an item",
		func(ctx *Context, in struct {
			ID string `arg:""`
		}) (map[string]any, error) {
			if !ctx.DryRun() {
				h.deleted = append(h.deleted, in.ID)
			}
			return map[string]any{"id": in.ID, "dry_run": ctx.DryRun()}, nil
		},
		Destructive(), SupportsDryRun())
	Command(app, "fail", "Always fails",
		func(ctx *Context, in struct{}) (Empty, error) { return Empty{}, fmt.Errorf("boom") },
		Exclude(ProjectionMCP, ProjectionSkill))
	Group(app, "tag", "Work with tags")
	Command(app, "tag add", "Add a tag",
		func(ctx *Context, in struct {
			Name string `arg:""`
		}) (Empty, error) {
			return Empty{}, nil
		}, Write())
	h.app = app
	return h
}

func (h *harness) run(args ...string) int {
	h.stdout.Reset()
	h.stderr.Reset()
	return h.app.Execute(args)
}

func (h *harness) errorJSON(t *testing.T) *Error {
	t.Helper()
	var got struct{ Error *Error }
	if err := json.Unmarshal(h.stderr.Bytes(), &got); err != nil || got.Error == nil {
		t.Fatalf("stderr is not a JSON error: %q (%v)", h.stderr.String(), err)
	}
	return got.Error
}

func TestKebab(t *testing.T) {
	for in, want := range map[string]string{"Limit": "limit", "MaxResults": "max-results", "ID": "id", "UserID": "user-id", "HTTPPort": "http-port"} {
		if got := kebab(in); got != want {
			t.Errorf("kebab(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBinding(t *testing.T) {
	h := newHarness(t, false, "")
	t.Setenv("TEST_TOKEN", "secret")
	if code := h.run("find", "alpha", "--limit", "3", "-t", "x", "--tag", "y,z", "--exact", "--sort", "name"); code != ExitOK {
		t.Fatalf("exit %d, stderr %s", code, h.stderr)
	}
	got := h.lastFind
	want := findInput{Query: "alpha", Limit: 3, Sort: "name", Tag: []string{"x", "y", "z"}, Token: "secret", Exact: true}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("input = %+v, want %+v", got, want)
	}

	h.run("find", "beta")
	if h.lastFind.Limit != 10 || h.lastFind.Exact || len(h.lastFind.Tag) != 0 {
		t.Errorf("defaults not applied or state leaked between runs: %+v", h.lastFind)
	}
}

func TestUsageErrors(t *testing.T) {
	h := newHarness(t, false, "")
	cases := []struct {
		args []string
		code string
	}{
		{[]string{"find"}, "MISSING_ARGUMENT"},
		{[]string{"find", "a", "--limit", "x"}, "INVALID_VALUE"},
		{[]string{"find", "a", "--sort", "size"}, "INVALID_VALUE"},
		{[]string{"find", "a", "b"}, "TOO_MANY_ARGUMENTS"},
		{[]string{"find", "a", "--nope"}, "INVALID_FLAG"},
		{[]string{"find", "a", "-o", "xml"}, "INVALID_OUTPUT"},
	}
	for _, tc := range cases {
		if code := h.run(tc.args...); code != ExitUsage {
			t.Errorf("%v: exit %d, want %d", tc.args, code, ExitUsage)
			continue
		}
		if e := h.errorJSON(t); e.Kind != KindUsage || e.Code != tc.code || e.Hint == "" {
			t.Errorf("%v: error = %+v, want usage/%s with a hint", tc.args, e, tc.code)
		}
	}
}

func TestOutputFormats(t *testing.T) {
	h := newHarness(t, false, "")
	h.run("find", "a")
	if want := `[{"id":"1","name":"alpha","tags":["a","b"],"count":3},{"id":"2","name":"beta","count":5}]` + "\n"; h.stdout.String() != want {
		t.Errorf("piped output = %q, want %q", h.stdout, want)
	}

	h.run("find", "a", "-o", "ndjson")
	if lines := strings.Split(strings.TrimSpace(h.stdout.String()), "\n"); len(lines) != 2 {
		t.Errorf("ndjson lines = %d, want 2: %q", len(lines), h.stdout)
	}

	h.run("find", "a", "--fields", "name,id")
	if want := `[{"name":"alpha","id":"1"},{"name":"beta","id":"2"}]` + "\n"; h.stdout.String() != want {
		t.Errorf("--fields output = %q, want %q", h.stdout, want)
	}

	if code := h.run("find", "a", "--fields", "nope"); code != ExitUsage || !strings.Contains(h.errorJSON(t).Hint, "count, id, name, tags") {
		t.Errorf("unknown field: exit %d, stderr %s", code, h.stderr)
	}

	h.run("find", "a", "--envelope")
	var env struct {
		OK   bool
		Data []item
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &env); err != nil || !env.OK || len(env.Data) != 2 {
		t.Errorf("envelope = %q (%v)", h.stdout, err)
	}
}

func TestTextOutputOnTerminal(t *testing.T) {
	h := newHarness(t, true, "")
	h.run("find", "a")
	lines := strings.Split(strings.TrimSpace(h.stdout.String()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "ID") || !strings.Contains(lines[0], "COUNT") || !strings.Contains(lines[1], "a, b") {
		t.Errorf("table = %q", h.stdout)
	}

	h.run("get", "1")
	if out := h.stdout.String(); !strings.Contains(out, "name:") || !strings.Contains(out, "alpha") {
		t.Errorf("key/value output = %q", out)
	}

	h.run("get", "1", "-o", "json")
	if !strings.Contains(h.stdout.String(), "\n  \"id\"") {
		t.Errorf("json on a terminal should be indented: %q", h.stdout)
	}
}

func TestErrors(t *testing.T) {
	h := newHarness(t, false, "")
	if code := h.run("get", "9"); code != ExitNotFound {
		t.Fatalf("exit %d, want %d", code, ExitNotFound)
	}
	if e := h.errorJSON(t); e.Kind != KindNotFound || e.Code != "ITEM_NOT_FOUND" || e.Hint != "run 'tool find'" {
		t.Errorf("error = %+v", e)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout must stay empty on error: %q", h.stdout)
	}

	if code := h.run("fail"); code != ExitError || h.errorJSON(t).Message != "boom" {
		t.Errorf("plain error: exit %d, stderr %s", code, h.stderr)
	}

	h.run("get", "9", "--envelope")
	if !strings.HasPrefix(h.stdout.String(), `{"error":`) && !strings.HasPrefix(h.stdout.String(), `{"ok":false`) {
		t.Errorf("envelope error should go to stdout: %q", h.stdout)
	}

	th := newHarness(t, true, "")
	th.run("get", "9")
	if got := th.stderr.String(); got != "Error: item 9 not found\nHint: run 'tool find'\n" {
		t.Errorf("human error = %q", got)
	}
}

func TestDestructive(t *testing.T) {
	h := newHarness(t, false, "")
	if code := h.run("delete", "1"); code != ExitConfirm || len(h.deleted) != 0 {
		t.Fatalf("without --yes: exit %d, deleted %v", code, h.deleted)
	}
	if e := h.errorJSON(t); e.Kind != KindConfirmationRequired || !strings.Contains(e.Hint, "--dry-run") {
		t.Errorf("error = %+v", e)
	}
	if code := h.run("delete", "1", "--dry-run"); code != ExitOK || len(h.deleted) != 0 || !strings.Contains(h.stdout.String(), `"dry_run":true`) {
		t.Errorf("--dry-run: exit %d, deleted %v, out %q", code, h.deleted, h.stdout)
	}
	if code := h.run("delete", "1", "--yes"); code != ExitOK || len(h.deleted) != 1 {
		t.Errorf("--yes: exit %d, deleted %v", code, h.deleted)
	}

	yes := newHarness(t, true, "y\n")
	if code := yes.run("delete", "2"); code != ExitOK || len(yes.deleted) != 1 || !strings.Contains(yes.stderr.String(), "Continue?") {
		t.Errorf("prompt yes: exit %d, deleted %v, stderr %q", code, yes.deleted, yes.stderr)
	}
	no := newHarness(t, true, "n\n")
	if code := no.run("delete", "2"); code != ExitConfirm || len(no.deleted) != 0 {
		t.Errorf("prompt no: exit %d, deleted %v", code, no.deleted)
	}
}

func TestNestedAndAliases(t *testing.T) {
	h := newHarness(t, false, "")
	if code := h.run("tag", "add", "x"); code != ExitOK || h.stdout.String() != "{}\n" {
		t.Errorf("tag add: exit %d, out %q, err %q", code, h.stdout, h.stderr)
	}
	if code := h.run("f", "alpha"); code != ExitOK || h.lastFind.Query != "alpha" {
		t.Errorf("alias: exit %d", code)
	}
}

func TestSchema(t *testing.T) {
	h := newHarness(t, false, "")
	h.run("schema")
	var m Manifest
	if err := json.Unmarshal(h.stdout.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Protocol != ManifestProtocol || m.Name != "tool" || m.Version != "1.2.3" || len(m.Commands) != 5 || m.ExitCodes["confirmation_required"] != ExitConfirm {
		t.Errorf("manifest = %+v", m)
	}

	h.run("schema", "find")
	var spec CommandSpec
	if err := json.Unmarshal(h.stdout.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	props := spec.Input["properties"].(map[string]any)
	limit := props["limit"].(map[string]any)
	sortKey := props["sort"].(map[string]any)
	if spec.Usage != "tool find <query> [flags]" || limit["default"] != float64(10) || len(sortKey["enum"].([]any)) != 2 ||
		fmt.Sprint(spec.Input["required"]) != "[query]" || spec.Output["type"] != "array" {
		t.Errorf("spec = %+v", spec)
	}
	if h.stdout.Len()/4 > 300 {
		t.Errorf("schema for one command is %d bytes; keep it under ~300 tokens", h.stdout.Len())
	}

	if code := h.run("schema", "nope"); code != ExitNotFound {
		t.Errorf("unknown command: exit %d", code)
	}
}

func TestSkillFiles(t *testing.T) {
	h := newHarness(t, false, "")
	files := h.app.SkillFiles()
	skill := files["SKILL.md"]
	for _, want := range []string{"name: tool\n", "### `tool find <query> [flags]`", "`--limit int` max results (default 10)", "Risk: **destructive**", "`--dry-run`", "tool find alpha"} {
		if !strings.Contains(skill, want) {
			t.Errorf("SKILL.md missing %q", want)
		}
	}
	if strings.Contains(skill, "Always fails") {
		t.Error("excluded command leaked into the skill")
	}

	big := New("big")
	for i := 0; i < 60; i++ {
		Command(big, fmt.Sprintf("group%d cmd", i%6), strings.Repeat("A long description. ", 8),
			func(ctx *Context, in findInput) (Empty, error) { return Empty{}, nil })
	}
	files = big.SkillFiles()
	if len(files) != 7 || !strings.Contains(files["SKILL.md"], "references/group0.md") || len(files["SKILL.md"])/4 > skillTokenBudget {
		t.Errorf("large skill should split into an index and 6 references; got %d files", len(files))
	}
}

func TestMCP(t *testing.T) {
	h := newHarness(t, false, "")
	session := connectMCP(t, h.app, MCPOptions{})

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, ","); got != "find,get,tag_add" {
		t.Errorf("tools = %s (destructive and excluded commands must be hidden)", got)
	}

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get", Arguments: map[string]any{"id": "1"}})
	if err != nil || res.IsError {
		t.Fatalf("call get: %v %+v", err, res)
	}
	if obj, ok := res.StructuredContent.(map[string]any); !ok || obj["name"] != "alpha" {
		t.Errorf("structured content = %#v", res.StructuredContent)
	}

	res, _ = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "find", Arguments: map[string]any{"query": "a", "limit": 2}})
	if res.IsError || h.lastFind.Limit != 2 || h.lastFind.Query != "a" {
		t.Errorf("call find: %+v, input %+v", res, h.lastFind)
	}

	res, _ = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get", Arguments: map[string]any{"id": "9"}})
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "ITEM_NOT_FOUND") {
		t.Errorf("error result = %+v", res)
	}

	res, _ = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "find", Arguments: map[string]any{"bogus": 1}})
	if !res.IsError {
		t.Error("unknown arguments must be rejected")
	}

	all := connectMCP(t, h.app, MCPOptions{AllowDestructive: true})
	res, err = all.CallTool(context.Background(), &mcp.CallToolParams{Name: "delete", Arguments: map[string]any{"id": "7", "dry-run": true}})
	if err != nil || res.IsError || len(h.deleted) != 0 {
		t.Errorf("dry-run delete: %v %+v deleted=%v", err, res, h.deleted)
	}
}

// connectMCP connects the official MCP SDK client to the built-in server,
// which checks protocol compatibility, including the server/discover fallback.
func connectMCP(t *testing.T, app *App, opts MCPOptions) *mcp.ClientSession {
	t.Helper()
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.ServeMCP(ctx, serverRead, serverWrite, opts) }()
	transport := &mcp.IOTransport{Reader: clientRead, Writer: clientWrite}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		session.Close()
		cancel()
		if err := <-done; err != nil {
			t.Errorf("ServeMCP: %v", err)
		}
	})
	return session
}

func TestMCPProtocol(t *testing.T) {
	h := newHarness(t, false, "")
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"server/discover"}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := h.app.ServeMCP(context.Background(), strings.NewReader(in), &out, MCPOptions{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{
		`"id":1,"error":{"code":-32601`,
		`"protocolVersion":"2024-11-05"`,
		`"id":3,"result":{}`,
		`"id":null,"error":{"code":-32700`,
		`"id":4,"error":{"code":-32602`,
		`"protocolVersion":"2025-11-25"`,
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d responses (notifications must not be answered):\n%s", len(lines), out.String())
	}
	for i, w := range want {
		if !strings.Contains(lines[i], w) {
			t.Errorf("response %d = %s, want it to contain %s", i, lines[i], w)
		}
	}
}

func TestRegistrationPanics(t *testing.T) {
	for name, register := range map[string]func(*App){
		"reserved command": func(a *App) {
			Command(a, "schema", "", func(*Context, struct{}) (Empty, error) { return Empty{}, nil })
		},
		"reserved flag": func(a *App) {
			Command(a, "x", "", func(*Context, struct{ Output string }) (Empty, error) { return Empty{}, nil })
		},
		"non-struct input": func(a *App) {
			Command(a, "x", "", func(*Context, string) (Empty, error) { return Empty{}, nil })
		},
		"bad default": func(a *App) {
			Command(a, "x", "", func(*Context, struct {
				N int `default:"abc"`
			}) (Empty, error) {
				return Empty{}, nil
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic")
				}
			}()
			register(New("t"))
		})
	}
}
