package axon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// agentHarness points the app at temporary home and config directories.
func agentHarness(t *testing.T, exe string) (*harness, string) {
	t.Helper()
	h := newHarness(t, false, "")
	root := t.TempDir()
	h.app.sys = sys{
		home:       func() (string, error) { return filepath.Join(root, "home"), nil },
		config:     func() (string, error) { return filepath.Join(root, "config"), nil },
		localData:  func() string { return filepath.Join(root, "local") },
		executable: func() (string, error) { return exe, nil },
	}
	return h, root
}

func readConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func agentResult(t *testing.T, h *harness) AgentResult {
	t.Helper()
	var res AgentResult
	if err := json.Unmarshal(h.stdout.Bytes(), &res); err != nil {
		t.Fatalf("stdout %q: %v (stderr %s)", h.stdout, err, h.stderr)
	}
	return res
}

func TestAgentInstallMCP(t *testing.T) {
	exe := filepath.Join("/usr", "local", "bin", "tool")
	h, root := agentHarness(t, exe)
	path := filepath.Join(root, "config", "Claude", "claude_desktop_config.json")

	if code := h.run("agent", "install", "claude-desktop", "--dry-run"); code != ExitOK {
		t.Fatalf("dry run: exit %d, %s", code, h.stderr)
	}
	if res := agentResult(t, h); res.Status != "installed" || !res.DryRun {
		t.Errorf("dry run result = %+v", res)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("--dry-run must not write anything")
	}

	// An existing config: other servers and settings must survive.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{"theme":"dark","mcpServers":{"other":{"command":"other"}}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("agent", "install", "claude-desktop"); code != ExitOK {
		t.Fatalf("install: exit %d, %s", code, h.stderr)
	}
	if res := agentResult(t, h); res.Status != "installed" || res.Path != path {
		t.Errorf("install result = %+v", res)
	}
	cfg := readConfig(t, path)
	servers := cfg["mcpServers"].(map[string]any)
	tool := servers["tool"].(map[string]any)
	if cfg["theme"] != "dark" || servers["other"] == nil || tool["command"] != exe || strings.Join(toStrings(tool["args"]), " ") != "mcp serve" {
		t.Errorf("config = %v", cfg)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != original {
		t.Errorf("backup = %q, want the original file", bak)
	}

	h.run("agent", "install", "claude-desktop")
	if res := agentResult(t, h); res.Status != "unchanged" {
		t.Errorf("reinstall status = %q, want unchanged", res.Status)
	}
	h.run("agent", "install", "claude-desktop", "--allow-destructive")
	if res := agentResult(t, h); res.Status != "updated" {
		t.Errorf("changed install status = %q, want updated", res.Status)
	}

	h.run("agent", "list")
	if !strings.Contains(h.stdout.String(), `"target":"claude-desktop","kind":"mcp"`) || !strings.Contains(h.stdout.String(), `"status":"installed"`) {
		t.Errorf("list = %s", h.stdout)
	}

	if code := h.run("agent", "uninstall", "claude-desktop"); code != ExitOK {
		t.Fatalf("uninstall: exit %d, %s", code, h.stderr)
	}
	servers = readConfig(t, path)["mcpServers"].(map[string]any)
	if _, ok := servers["tool"]; ok || servers["other"] == nil {
		t.Errorf("uninstall must remove only this CLI: %v", servers)
	}
	h.run("agent", "uninstall", "claude-desktop")
	if res := agentResult(t, h); res.Status != "not_installed" {
		t.Errorf("second uninstall status = %q", res.Status)
	}
}

func TestClaudeDesktopConfigLocation(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "local")
	s := sys{
		home:      func() (string, error) { return filepath.Join(root, "home"), nil },
		config:    func() (string, error) { return filepath.Join(root, "config"), nil },
		localData: func() string { return local },
	}
	mkdir := func(parts ...string) string {
		p := filepath.Join(append([]string{local}, parts...)...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	check := func(want string) {
		t.Helper()
		if got, _ := claudeDesktopConfig(s); got != want {
			t.Errorf("config path = %s, want %s", got, want)
		}
	}

	check(filepath.Join(root, "config", "Claude", "claude_desktop_config.json"))

	pkg := mkdir("Packages", "Claude_pzs8sxrjxfjjc")
	check(filepath.Join(pkg, "LocalCache", "Roaming", "Claude", "claude_desktop_config.json"))

	// Current versions move their data to Claude-Data, which wins.
	data := mkdir("Claude-Data")
	sub := mkdir("Claude-Data", "claude_pzs8sxrjxfjjc")
	check(filepath.Join(sub, "claude_desktop_config.json"))

	// When several config files exist, the one written last is in use.
	older := filepath.Join(data, "claude_desktop_config.json")
	newer := filepath.Join(sub, "claude_desktop_config.json")
	for i, p := range []string{older, newer} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(time.Duration(i-2) * time.Hour)
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
	}
	check(newer)
}

func TestClaudeDesktopInstallNote(t *testing.T) {
	h, _ := agentHarness(t, "/usr/local/bin/tool")
	if code := h.run("agent", "install", "claude-desktop"); code != ExitOK {
		t.Fatalf("install: exit %d, %s", code, h.stderr)
	}
	if res := agentResult(t, h); !strings.Contains(res.Note, "Quit Claude Desktop") {
		t.Errorf("result = %+v, want a restart note", res)
	}
}

func TestAgentInstallRefusals(t *testing.T) {
	h, root := agentHarness(t, filepath.Join(os.TempDir(), "go-build123", "b001", "exe", "tool"))
	if code := h.run("agent", "install", "cursor"); code != ExitUsage || h.errorJSON(t).Code != "TEMPORARY_BINARY" {
		t.Errorf("temporary binary: exit %d, %s", code, h.stderr)
	}

	h, root = agentHarness(t, "/usr/local/bin/tool")
	path := filepath.Join(root, "home", ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.run("agent", "install", "cursor"); code != ExitError || h.errorJSON(t).Code != "INVALID_CONFIG" {
		t.Errorf("broken config: exit %d, %s", code, h.stderr)
	}
	if b, _ := os.ReadFile(path); string(b) != "{broken" {
		t.Error("a broken config must be left untouched")
	}

	if code := h.run("agent", "install", "vim"); code != ExitUsage || !strings.Contains(h.errorJSON(t).Hint, "claude-desktop") {
		t.Errorf("unknown agent: exit %d, %s", code, h.stderr)
	}
}

func TestAgentInstallSkill(t *testing.T) {
	h, root := agentHarness(t, "/usr/local/bin/tool")
	dir := filepath.Join(root, "home", ".claude", "skills", "tool")
	if code := h.run("agent", "install", "claude-code"); code != ExitOK {
		t.Fatalf("install: exit %d, %s", code, h.stderr)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "SKILL.md")); err != nil || !strings.Contains(string(b), "name: tool") {
		t.Fatalf("SKILL.md: %v", err)
	}
	h.run("agent", "install", "claude-code")
	if res := agentResult(t, h); res.Status != "updated" {
		t.Errorf("reinstall status = %q", res.Status)
	}
	h.run("agent", "uninstall", "claude-code")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("uninstall must remove the skill directory")
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
