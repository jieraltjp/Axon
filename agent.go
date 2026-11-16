package axon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// agentTarget is an agent the application can register itself with: either
// as an MCP server in the agent's config file, or as a skill directory.
type agentTarget struct {
	name   string
	kind   string // "mcp" or "skill"
	format string // config file format for "mcp": "json" (default) or "toml"
	path   func(sys) (string, error)
	note   string // what the user must do for the change to take effect
}

// claudeDesktopConfig locates Claude Desktop's config file. The Windows MSIX
// build (official installer, Microsoft Store, WinGet) runs in a container
// and reads a virtualized copy under %LOCALAPPDATA%\Packages\Claude_*,
// not the documented %APPDATA%\Claude path.
func claudeDesktopConfig(s sys) (string, error) {
	if local := s.localData(); local != "" {
		matches, _ := filepath.Glob(filepath.Join(local, "Packages", "Claude_*"))
		for _, m := range matches {
			if info, err := os.Stat(m); err == nil && info.IsDir() {
				return filepath.Join(m, "LocalCache", "Roaming", "Claude", "claude_desktop_config.json"), nil
			}
		}
	}
	dir, err := s.config()
	return filepath.Join(dir, "Claude", "claude_desktop_config.json"), err
}

// sys gives access to the user's environment; tests replace it.
type sys struct {
	home       func() (string, error)
	config     func() (string, error)
	localData  func() string // %LOCALAPPDATA% on Windows, empty elsewhere
	executable func() (string, error)
}

var defaultSys = sys{
	home:      os.UserHomeDir,
	config:    os.UserConfigDir,
	localData: func() string { return os.Getenv("LOCALAPPDATA") },
	executable: func() (string, error) {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(exe)
	},
}

var agentTargets = []agentTarget{
	{name: "claude-code", kind: "skill", path: func(s sys) (string, error) {
		home, err := s.home()
		return filepath.Join(home, ".claude", "skills"), err
	}},
	{name: "claude-desktop", kind: "mcp", path: claudeDesktopConfig,
		note: "Quit Claude Desktop completely (including from the system tray) and start it again to load the change."},
	{name: "codex", kind: "mcp", format: "toml", path: func(s sys) (string, error) {
		if dir := os.Getenv("CODEX_HOME"); dir != "" {
			return filepath.Join(dir, "config.toml"), nil
		}
		home, err := s.home()
		return filepath.Join(home, ".codex", "config.toml"), err
	}},
	{name: "cursor", kind: "mcp", path: func(s sys) (string, error) {
		home, err := s.home()
		return filepath.Join(home, ".cursor", "mcp.json"), err
	}},
}

// AgentResult reports what agent install, uninstall or list did for a target.
type AgentResult struct {
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Status string `json:"status"`
	DryRun bool   `json:"dry_run,omitempty"`
	Note   string `json:"note,omitempty"`
}

func findTarget(name string) (agentTarget, error) {
	for _, t := range agentTargets {
		if t.name == name {
			return t, nil
		}
	}
	return agentTarget{}, Usage("UNKNOWN_AGENT", "unknown agent %q", name).
		WithHint("supported agents: " + agentNames())
}

func (a *App) addAgentCommands() {
	agent := &cobra.Command{Use: "agent", Short: "Register this CLI with AI agents", GroupID: groupAgent}

	var dryRun, allowDestructive bool
	install := &cobra.Command{
		Use:     "install <agent>",
		Short:   "Register this CLI with an agent (" + agentNames() + ")",
		Example: "  " + a.name + " agent install claude-desktop\n  " + a.name + " agent install claude-code --dry-run",
		Args:    a.exactlyOneAgent,
		RunE: func(cc *cobra.Command, args []string) error {
			res, err := a.agentInstall(args[0], dryRun, allowDestructive)
			if err != nil {
				return err
			}
			return a.writeOutput(reflect.TypeFor[AgentResult](), res)
		},
	}
	install.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would change without writing anything")
	install.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "Expose destructive commands to MCP agents")

	var uninstallDryRun bool
	uninstall := &cobra.Command{
		Use:   "uninstall <agent>",
		Short: "Remove this CLI from an agent",
		Args:  a.exactlyOneAgent,
		RunE: func(cc *cobra.Command, args []string) error {
			res, err := a.agentUninstall(args[0], uninstallDryRun)
			if err != nil {
				return err
			}
			return a.writeOutput(reflect.TypeFor[AgentResult](), res)
		},
	}
	uninstall.Flags().BoolVar(&uninstallDryRun, "dry-run", false, "Show what would change without writing anything")

	list := &cobra.Command{
		Use:   "list",
		Short: "Show which agents this CLI is registered with",
		Args:  cobra.NoArgs,
		RunE: func(cc *cobra.Command, args []string) error {
			var results []AgentResult
			for _, t := range agentTargets {
				res, err := a.agentStatus(t)
				if err != nil {
					return err
				}
				results = append(results, res)
			}
			return a.writeOutput(reflect.TypeFor[[]AgentResult](), results)
		},
	}

	agent.AddCommand(install, uninstall, list)
	a.root.AddCommand(agent)
}

func (a *App) exactlyOneAgent(cc *cobra.Command, args []string) error {
	if len(args) != 1 {
		return Usage("MISSING_ARGUMENT", "missing required <agent>").
			WithHint("supported agents: " + agentNames())
	}
	return nil
}

func (a *App) agentInstall(name string, dryRun, allowDestructive bool) (AgentResult, error) {
	t, err := findTarget(name)
	if err != nil {
		return AgentResult{}, err
	}
	path, err := t.path(a.sys)
	if err != nil {
		return AgentResult{}, Errorf("NO_CONFIG_DIR", "cannot locate the %s configuration: %v", name, err)
	}
	res := AgentResult{Target: t.name, Kind: t.kind, DryRun: dryRun, Note: t.note}

	if t.kind == "skill" {
		dir := filepath.Join(path, skillName(a.name))
		res.Path = dir
		res.Status = "installed"
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil {
			res.Status = "updated"
		}
		if !dryRun {
			if _, err := a.exportSkill(dir); err != nil {
				return res, err
			}
		}
		return res, nil
	}

	res.Path = path
	exe, err := a.sys.executable()
	if err != nil {
		return res, Errorf("NO_EXECUTABLE", "cannot locate this program: %v", err)
	}
	if isTemporaryBuild(exe) {
		return res, Usage("TEMPORARY_BINARY", "%s is a temporary build and will disappear", exe).
			WithHint("install the CLI first (for example with 'go install') and run 'agent install' from the installed binary")
	}
	args := []string{"mcp", "serve"}
	if allowDestructive {
		args = append(args, "--allow-destructive")
	}
	if t.format == "toml" {
		lines, existed, crlf, err := readText(path)
		if err != nil {
			return res, err
		}
		out, status := tomlUpsert(lines, a.name, exe, args)
		res.Status = status
		if status == "unchanged" || dryRun {
			return res, nil
		}
		return res, writeWithBackup(path, joinText(out, crlf), existed)
	}
	entry := map[string]any{"command": exe, "args": args}

	cfg, existed, err := readJSONObject(path)
	if err != nil {
		return res, err
	}
	servers, err := mcpServers(cfg, path)
	if err != nil {
		return res, err
	}
	switch old, ok := servers[a.name]; {
	case !ok:
		res.Status = "installed"
	case reflect.DeepEqual(normalizeJSON(old), normalizeJSON(entry)):
		res.Status = "unchanged"
		return res, nil
	default:
		res.Status = "updated"
	}
	servers[a.name] = entry
	if dryRun {
		return res, nil
	}
	return res, writeJSONObject(path, cfg, existed)
}

func (a *App) agentUninstall(name string, dryRun bool) (AgentResult, error) {
	t, err := findTarget(name)
	if err != nil {
		return AgentResult{}, err
	}
	res, err := a.agentStatus(t)
	if err != nil {
		return res, err
	}
	res.DryRun = dryRun
	if res.Status == "not_installed" {
		return res, nil
	}
	res.Status = "removed"
	if dryRun {
		return res, nil
	}
	if t.kind == "skill" {
		if err := os.RemoveAll(res.Path); err != nil {
			return res, Errorf("WRITE_FAILED", "cannot remove %s: %v", res.Path, err)
		}
		return res, nil
	}
	if t.format == "toml" {
		lines, existed, crlf, err := readText(res.Path)
		if err != nil {
			return res, err
		}
		out, _ := tomlRemove(lines, a.name)
		return res, writeWithBackup(res.Path, joinText(out, crlf), existed)
	}
	cfg, existed, err := readJSONObject(res.Path)
	if err != nil {
		return res, err
	}
	servers, err := mcpServers(cfg, res.Path)
	if err != nil {
		return res, err
	}
	delete(servers, a.name)
	return res, writeJSONObject(res.Path, cfg, existed)
}

func (a *App) agentStatus(t agentTarget) (AgentResult, error) {
	path, err := t.path(a.sys)
	if err != nil {
		return AgentResult{}, Errorf("NO_CONFIG_DIR", "cannot locate the %s configuration: %v", t.name, err)
	}
	res := AgentResult{Target: t.name, Kind: t.kind, Path: path, Status: "not_installed"}
	if t.kind == "skill" {
		res.Path = filepath.Join(path, skillName(a.name))
		if _, err := os.Stat(filepath.Join(res.Path, "SKILL.md")); err == nil {
			res.Status = "installed"
		}
		return res, nil
	}
	if t.format == "toml" {
		lines, _, _, err := readText(path)
		if err != nil {
			return res, err
		}
		if _, _, _, ok := tomlTable(lines, a.name); ok {
			res.Status = "installed"
		}
		return res, nil
	}
	cfg, _, err := readJSONObject(path)
	if err != nil {
		return res, err
	}
	if servers, ok := cfg["mcpServers"].(map[string]any); ok {
		if _, ok := servers[a.name]; ok {
			res.Status = "installed"
		}
	}
	return res, nil
}

// isTemporaryBuild reports whether exe was built by "go run" or "go test".
func isTemporaryBuild(exe string) bool {
	return strings.Contains(filepath.ToSlash(exe), "/go-build")
}

// readJSONObject reads a JSON object file; a missing file is an empty object.
func readJSONObject(path string) (map[string]any, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, false, nil
	}
	if err != nil {
		return nil, false, Errorf("READ_FAILED", "cannot read %s: %v", path, err)
	}
	cfg := map[string]any{}
	if len(strings.TrimSpace(string(b))) > 0 {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil, true, Errorf("INVALID_CONFIG", "%s is not a valid JSON object: %v", path, err).
				WithHint("fix or move the file, then try again; nothing was changed")
		}
	}
	return cfg, true, nil
}

func mcpServers(cfg map[string]any, path string) (map[string]any, error) {
	switch s := cfg["mcpServers"].(type) {
	case nil:
		servers := map[string]any{}
		cfg["mcpServers"] = servers
		return servers, nil
	case map[string]any:
		return s, nil
	}
	return nil, Errorf("INVALID_CONFIG", "%s: \"mcpServers\" is not an object", path).
		WithHint("fix the file, then try again; nothing was changed")
}

// writeJSONObject writes cfg to path, keeping a .bak copy of an existing file.
func writeJSONObject(path string, cfg map[string]any, existed bool) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Errorf("ENCODE_FAILED", "cannot encode %s: %v", path, err)
	}
	return writeWithBackup(path, append(b, '\n'), existed)
}

// writeWithBackup writes data to path, first copying an existing file to
// path.bak.
func writeWithBackup(path string, data []byte, existed bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Errorf("WRITE_FAILED", "cannot create %s: %v", filepath.Dir(path), err)
	}
	if existed {
		old, err := os.ReadFile(path)
		if err == nil {
			err = os.WriteFile(path+".bak", old, 0o600)
		}
		if err != nil {
			return Errorf("WRITE_FAILED", "cannot back up %s: %v", path, err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Errorf("WRITE_FAILED", "cannot write %s: %v", path, err)
	}
	return nil
}

// normalizeJSON round-trips v so values read from disk compare equal to
// values built in memory.
func normalizeJSON(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// agentNames lists supported agents, for help text.
func agentNames() string {
	var names []string
	for _, t := range agentTargets {
		names = append(names, t.name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
