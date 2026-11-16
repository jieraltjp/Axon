// Package axon is an agent-native CLI framework. A command is defined once as
// a typed Go function and is served to humans (help, tables, prompts), to
// agents and scripts (JSON, structured errors, stable exit codes, a schema
// command, generated skills) and to MCP clients.
package axon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// App is a command-line application.
type App struct {
	name        string
	version     string
	description string

	root     *cobra.Command
	commands []*command
	groups   map[string]*cobra.Command

	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
	stdinTTY  bool
	stdoutTTY bool

	// Global flags, re-parsed on every execution.
	output   string
	fields   string
	envelope bool
}

// AppOption configures an App.
type AppOption func(*App)

// Version sets the application version.
func Version(v string) AppOption { return func(a *App) { a.version = v } }

// Description sets the one-line application description.
func Description(d string) AppOption { return func(a *App) { a.description = d } }

const (
	groupCommands = "commands"
	groupAgent    = "agent"
)

// New creates an application named name.
func New(name string, opts ...AppOption) *App {
	a := &App{
		name:      name,
		groups:    map[string]*cobra.Command{},
		stdin:     os.Stdin,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		stdinTTY:  isTerminal(os.Stdin),
		stdoutTTY: isTerminal(os.Stdout),
	}
	for _, opt := range opts {
		opt(a)
	}
	a.root = &cobra.Command{
		Use:           name,
		Short:         a.description,
		Version:       a.version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	a.root.AddGroup(
		&cobra.Group{ID: groupCommands, Title: "Commands:"},
		&cobra.Group{ID: groupAgent, Title: "Agent Commands:"},
	)
	a.root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return Usage("INVALID_FLAG", "%v", err).WithHint(helpHint(c))
	})
	pf := a.root.PersistentFlags()
	pf.StringVarP(&a.output, "output", "o", "", "Output format: text, json or ndjson (default: text on a terminal, json otherwise)")
	pf.StringVar(&a.fields, "fields", "", "Comma-separated fields to keep in the output")
	pf.BoolVar(&a.envelope, "envelope", false, `Wrap output as {"ok":...,"data":...} or {"ok":false,"error":...}`)
	a.addBuiltins()
	return a
}

func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// Cobra returns the underlying root command for customization.
func (a *App) Cobra() *cobra.Command { return a.root }

// Run executes the application with os.Args and exits with the resulting code.
func (a *App) Run() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := a.ExecuteContext(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

// Execute runs the application with args and returns the exit code.
func (a *App) Execute(args []string) int {
	return a.ExecuteContext(context.Background(), args)
}

// ExecuteContext is like Execute with a context.
func (a *App) ExecuteContext(ctx context.Context, args []string) int {
	a.output, a.fields, a.envelope = "", "", false
	for _, c := range a.commands {
		c.reset()
	}
	a.root.SetArgs(args)
	a.root.SetIn(a.stdin)
	a.root.SetOut(a.stdout)
	a.root.SetErr(a.stderr)
	cmd, err := a.root.ExecuteContextC(ctx)
	if err == nil {
		return ExitOK
	}
	var e *Error
	if !errors.As(err, &e) {
		// Errors not produced by Axon come from cobra's argument parsing.
		e = Usage("INVALID_USAGE", "%v", err).WithHint(helpHint(cmd))
	}
	a.writeError(e)
	return e.ExitCode()
}

// Group sets the description of a command group such as "tag" in "tag add".
func Group(app *App, path, description string) {
	app.group(strings.Fields(path)).Short = description
}

// group returns the cobra command for a path prefix, creating intermediate
// group commands as needed.
func (a *App) group(path []string) *cobra.Command {
	parent := a.root
	for i := range path {
		key := strings.Join(path[:i+1], " ")
		g, ok := a.groups[key]
		if !ok {
			g = &cobra.Command{Use: path[i], Short: fmt.Sprintf("%s commands", path[i])}
			if i == 0 {
				g.GroupID = groupCommands
			}
			parent.AddCommand(g)
			a.groups[key] = g
		}
		parent = g
	}
	return parent
}

func helpHint(c *cobra.Command) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("run '%s --help'", c.CommandPath())
}
