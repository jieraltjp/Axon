package axon

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// addBuiltins registers the agent-facing commands every Axon app has.
func (a *App) addBuiltins() {
	a.root.AddCommand(&cobra.Command{
		Use:     "schema [command]",
		Short:   "Print the machine-readable description of commands",
		GroupID: groupAgent,
		Example: "  " + a.name + " schema\n  " + a.name + " schema <command>",
		RunE: func(cc *cobra.Command, args []string) error {
			if len(args) == 0 {
				return a.writeData(a.Manifest())
			}
			name := strings.Join(args, " ")
			c := a.find(name)
			if c == nil {
				return NotFound("UNKNOWN_COMMAND", "unknown command %q", name).
					WithHint("run '" + a.name + " schema' to list commands")
			}
			return a.writeData(a.commandSpec(c))
		},
	})

	skills := &cobra.Command{Use: "skills", Short: "Generate Agent Skills", GroupID: groupAgent}
	skills.AddCommand(&cobra.Command{
		Use:     "export [dir]",
		Short:   "Write an Agent Skill (SKILL.md) for this CLI",
		Example: "  " + a.name + " skills export ~/.claude/skills",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cc *cobra.Command, args []string) error {
			dir := "skills"
			if len(args) == 1 {
				dir = args[0]
			}
			paths, err := a.exportSkill(filepath.Join(dir, skillName(a.name)))
			if err != nil {
				return err
			}
			return a.writeOutput(reflect.TypeFor[[]string](), paths)
		},
	})
	a.root.AddCommand(skills)

	var allowDestructive bool
	mcpCmd := &cobra.Command{Use: "mcp", Short: "Serve commands over the Model Context Protocol", GroupID: groupAgent}
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Run an MCP server on stdio",
		RunE: func(cc *cobra.Command, args []string) error {
			err := a.ServeMCP(cc.Context(), a.stdin, a.stdout, MCPOptions{AllowDestructive: allowDestructive})
			if err != nil {
				return Unavailable("MCP_FAILED", "mcp server: %v", err)
			}
			return nil
		},
	}
	serve.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "Expose destructive commands as tools")
	mcpCmd.AddCommand(serve)
	a.root.AddCommand(mcpCmd)

	a.addAgentCommands()
}

// writeData writes a document such as the manifest. It is always JSON, since
// a table would lose its structure.
func (a *App) writeData(v any) error {
	if a.envelope {
		return a.writeJSON(map[string]any{"ok": true, "data": v})
	}
	return a.writeJSON(v)
}

func (a *App) exportSkill(dir string) ([]string, error) {
	files := a.SkillFiles()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var written []string
	for _, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, Errorf("WRITE_FAILED", "cannot create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil {
			return nil, Errorf("WRITE_FAILED", "cannot write %s: %v", path, err)
		}
		written = append(written, path)
	}
	return written, nil
}
