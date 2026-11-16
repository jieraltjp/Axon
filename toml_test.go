package axon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexConfig = `# my codex settings
model = "gpt-5"

[mcp_servers.other]
command = "other"

[mcp_servers.tool]
command = "old"
args = []

[mcp_servers.tool.env]
TOKEN = "x"

[profiles.fast]
model = "mini"
`

func lines(s string) []string { return strings.Split(strings.TrimSuffix(s, "\n"), "\n") }

func TestTOMLUpsert(t *testing.T) {
	out, status := tomlUpsert(lines(codexConfig), "tool", `C:\bin\tool.exe`, []string{"mcp", "serve"})
	got := strings.Join(out, "\n") + "\n"
	want := strings.Replace(codexConfig, "command = \"old\"\nargs = []",
		"command = \"C:\\\\bin\\\\tool.exe\"\nargs = [\"mcp\", \"serve\"]", 1)
	if status != "updated" || got != want {
		t.Errorf("status %q, got:\n%s\nwant:\n%s", status, got, want)
	}

	if _, status := tomlUpsert(out, "tool", `C:\bin\tool.exe`, []string{"mcp", "serve"}); status != "unchanged" {
		t.Errorf("second upsert status = %q, want unchanged", status)
	}

	added, status := tomlUpsert(lines("model = \"gpt-5\"\n"), "my tool", "/bin/t", []string{"mcp", "serve"})
	if want := "model = \"gpt-5\"\n\n[mcp_servers.\"my tool\"]\ncommand = \"/bin/t\"\nargs = [\"mcp\", \"serve\"]"; status != "installed" || strings.Join(added, "\n") != want {
		t.Errorf("append: status %q, got %q", status, strings.Join(added, "\n"))
	}
	if _, _, _, ok := tomlTable(added, "my tool"); !ok {
		t.Error("quoted table name not found")
	}

	empty, status := tomlUpsert(nil, "tool", "/bin/t", nil)
	if status != "installed" || empty[0] != "[mcp_servers.tool]" {
		t.Errorf("empty file: %q %q", status, empty)
	}
}

func TestTOMLRemove(t *testing.T) {
	out, removed := tomlRemove(lines(codexConfig), "tool")
	want := "# my codex settings\nmodel = \"gpt-5\"\n\n[mcp_servers.other]\ncommand = \"other\"\n\n[profiles.fast]\nmodel = \"mini\""
	if !removed || strings.Join(out, "\n") != want {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(out, "\n"), want)
	}
	if _, removed := tomlRemove(out, "tool"); removed {
		t.Error("removing a missing table must report false")
	}
}

func TestAgentInstallCodex(t *testing.T) {
	h, root := agentHarness(t, `C:\bin\tool.exe`)
	t.Setenv("CODEX_HOME", "")
	path := filepath.Join(root, "home", ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := strings.ReplaceAll("model = \"gpt-5\"\n", "\n", "\r\n")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := h.run("agent", "install", "codex"); code != ExitOK || agentResult(t, h).Status != "installed" {
		t.Fatalf("install: exit %d, out %s, err %s", code, h.stdout, h.stderr)
	}
	b, _ := os.ReadFile(path)
	if want := "model = \"gpt-5\"\r\n\r\n[mcp_servers.tool]\r\ncommand = \"C:\\\\bin\\\\tool.exe\"\r\nargs = [\"mcp\", \"serve\"]\r\n"; string(b) != want {
		t.Errorf("config.toml = %q, want %q (CRLF preserved)", b, want)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != original {
		t.Errorf("backup = %q", bak)
	}

	h.run("agent", "list")
	if !strings.Contains(h.stdout.String(), `"target":"codex","kind":"mcp","path":`) || strings.Count(h.stdout.String(), `"installed"`) != 1 {
		t.Errorf("list = %s", h.stdout)
	}

	h.run("agent", "uninstall", "codex")
	if b, _ := os.ReadFile(path); string(b) != original {
		t.Errorf("after uninstall = %q, want the original %q", b, original)
	}
}
