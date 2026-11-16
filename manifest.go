package axon

import "strings"

// ManifestProtocol identifies the manifest format printed by the schema command.
const ManifestProtocol = "axon/v0"

// Manifest is the machine-readable description of an application. It is what
// "<app> schema" prints, and the contract other tools build on.
type Manifest struct {
	Protocol    string         `json:"protocol"`
	Name        string         `json:"name"`
	Version     string         `json:"version,omitempty"`
	Description string         `json:"description,omitempty"`
	Commands    []CommandSpec  `json:"commands"`
	ExitCodes   map[string]int `json:"exit_codes"`
}

// CommandSpec describes one command.
type CommandSpec struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Usage       string   `json:"usage"`
	Risk        Risk     `json:"risk"`
	DryRun      bool     `json:"dry_run,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Args        []string `json:"args,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	Input       Schema   `json:"input"`
	Output      Schema   `json:"output"`
}

// Manifest describes every visible command.
func (a *App) Manifest() Manifest {
	m := Manifest{
		Protocol:    ManifestProtocol,
		Name:        a.name,
		Version:     a.version,
		Description: a.description,
		Commands:    []CommandSpec{},
		ExitCodes:   map[string]int{"ok": ExitOK},
	}
	for kind, code := range exitCodes {
		m.ExitCodes[string(kind)] = code
	}
	for _, c := range a.commands {
		if !c.hidden {
			m.Commands = append(m.Commands, a.commandSpec(c))
		}
	}
	return m
}

func (a *App) commandSpec(c *command) CommandSpec {
	spec := CommandSpec{
		Name:        c.name(),
		Description: c.description,
		Usage:       a.usage(c),
		Risk:        c.risk,
		DryRun:      c.dryRunnable,
		Aliases:     c.aliases,
		Examples:    c.examples,
		Input:       c.input.inputSchema(),
		Output:      outputSchema(c.outType),
	}
	for _, p := range c.input.args {
		spec.Args = append(spec.Args, p.name)
	}
	return spec
}

// usage renders the full invocation, e.g. "notes search <query> [flags]".
func (a *App) usage(c *command) string {
	parts := append([]string{a.name}, c.path[:len(c.path)-1]...)
	parts = append(parts, c.use())
	if len(c.input.flags) > 0 || c.risk == RiskDestructive || c.dryRunnable {
		parts = append(parts, "[flags]")
	}
	return strings.Join(parts, " ")
}

// find returns the visible command whose name or alias matches name.
func (a *App) find(name string) *command {
	for _, c := range a.commands {
		if c.hidden {
			continue
		}
		if c.name() == name {
			return c
		}
		prefix := strings.Join(c.path[:len(c.path)-1], " ")
		for _, alias := range c.aliases {
			if strings.TrimSpace(prefix+" "+alias) == name {
				return c
			}
		}
	}
	return nil
}
