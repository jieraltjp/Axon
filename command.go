package axon

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
)

// Risk tells humans, agents and MCP clients what a command may change.
type Risk string

const (
	RiskRead        Risk = "read"
	RiskWrite       Risk = "write"
	RiskDestructive Risk = "destructive"
)

// Projection is an interface a command can be exposed through besides the CLI.
type Projection string

const (
	ProjectionSkill Projection = "skill"
	ProjectionMCP   Projection = "mcp"
)

// Empty is the output type of commands that return no data.
type Empty struct{}

type command struct {
	path        []string
	description string
	risk        Risk
	examples    []string
	aliases     []string
	hidden      bool
	excluded    map[Projection]bool
	dryRunnable bool

	input   *inputSpec
	outType reflect.Type
	run     func(*Context, reflect.Value) (any, error)

	cobra  *cobra.Command
	raw    map[string]*rawValue
	yes    bool
	dryRun bool
}

func (c *command) name() string { return strings.Join(c.path, " ") }

func (c *command) visibleIn(p Projection) bool { return !c.hidden && !c.excluded[p] }

func (c *command) reset() {
	c.yes, c.dryRun = false, false
	for _, rv := range c.raw {
		rv.vals = nil
	}
}

// CommandOption configures a command.
type CommandOption func(*command)

// Write marks a command as modifying state without destroying anything.
func Write() CommandOption { return func(c *command) { c.risk = RiskWrite } }

// Destructive marks a command as destroying data or otherwise hard to undo.
// It requires confirmation: an interactive prompt on a terminal, or --yes.
// MCP servers hide destructive commands unless explicitly allowed.
func Destructive() CommandOption { return func(c *command) { c.risk = RiskDestructive } }

// SupportsDryRun adds a --dry-run flag. The command must honor Context.DryRun.
func SupportsDryRun() CommandOption { return func(c *command) { c.dryRunnable = true } }

// Example adds usage examples, shown in help, schema and skills.
func Example(examples ...string) CommandOption {
	return func(c *command) { c.examples = append(c.examples, examples...) }
}

// Aliases adds alternative names for the command.
func Aliases(aliases ...string) CommandOption {
	return func(c *command) { c.aliases = append(c.aliases, aliases...) }
}

// Hidden hides the command from help, schema, skills and MCP.
func Hidden() CommandOption { return func(c *command) { c.hidden = true } }

// Exclude keeps the command out of the given projections.
func Exclude(projections ...Projection) CommandOption {
	return func(c *command) {
		for _, p := range projections {
			c.excluded[p] = true
		}
	}
}

// Context is passed to every command handler.
type Context struct {
	context.Context
	dryRun bool
	via    string
	stderr io.Writer
}

// DryRun reports whether the caller asked for a preview without changes.
func (c *Context) DryRun() bool { return c.dryRun }

// Via reports how the command was invoked: "cli" or "mcp".
func (c *Context) Via() string { return c.via }

// Logf writes a diagnostic line to stderr, keeping stdout clean for data.
func (c *Context) Logf(format string, args ...any) {
	fmt.Fprintf(c.stderr, format+"\n", args...)
}

// Command registers a command. path may contain spaces to create subcommands
// ("tag add"). The input type In must be a struct; its fields become arguments
// and flags (see the struct tags documented on parseInput). Out is any value
// that encodes to JSON.
func Command[In, Out any](app *App, path, description string, fn func(*Context, In) (Out, error), opts ...CommandOption) {
	spec, err := parseInput(reflect.TypeFor[In]())
	if err != nil {
		panic(fmt.Sprintf("axon: command %q: %v", path, err))
	}
	c := &command{
		path:        strings.Fields(path),
		description: description,
		risk:        RiskRead,
		excluded:    map[Projection]bool{},
		input:       spec,
		outType:     reflect.TypeFor[Out](),
		run: func(ctx *Context, in reflect.Value) (any, error) {
			return fn(ctx, in.Interface().(In))
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	if len(c.path) == 0 {
		panic("axon: empty command path")
	}
	app.add(c)
}

var reservedTopLevel = map[string]bool{"schema": true, "skills": true, "mcp": true, "agent": true, "completion": true, "help": true}

func (a *App) add(c *command) {
	if len(c.path) == 1 && reservedTopLevel[c.path[0]] {
		panic(fmt.Sprintf("axon: command name %q is reserved", c.path[0]))
	}
	reserved := map[string]bool{"output": true, "fields": true, "envelope": true, "help": true, "yes": true, "dry-run": true}
	for _, p := range c.input.flags {
		if reserved[p.name] {
			panic(fmt.Sprintf("axon: command %q: flag name %q is reserved", c.name(), p.name))
		}
		if p.short == "o" || p.short == "y" || p.short == "h" {
			panic(fmt.Sprintf("axon: command %q: short flag %q is reserved", c.name(), p.short))
		}
	}

	cc := &cobra.Command{
		Use:     c.use(),
		Short:   c.description,
		Aliases: c.aliases,
		Hidden:  c.hidden,
		Example: "  " + strings.Join(c.examples, "\n  "),
		Args:    c.validateArgs,
		RunE: func(cc *cobra.Command, args []string) error {
			return a.execute(cc.Context(), c, args)
		},
	}
	if len(c.examples) == 0 {
		cc.Example = ""
	}
	c.raw = map[string]*rawValue{}
	for _, p := range c.input.flags {
		rv := &rawValue{typ: typeName(p.typ)}
		f := cc.Flags().VarPF(rv, p.name, p.short, p.usage())
		if p.typ.Kind() == reflect.Bool {
			f.NoOptDefVal = "true"
		}
		if p.hasDef {
			f.DefValue = p.def
		}
		c.raw[p.name] = rv
	}
	if c.risk == RiskDestructive {
		cc.Flags().BoolVarP(&c.yes, "yes", "y", false, "Confirm this destructive action")
	}
	if c.dryRunnable {
		cc.Flags().BoolVar(&c.dryRun, "dry-run", false, "Show what would happen without making changes")
	}
	parent := a.group(c.path[:len(c.path)-1])
	if parent == a.root {
		cc.GroupID = groupCommands
	}
	parent.AddCommand(cc)
	c.cobra = cc
	a.commands = append(a.commands, c)
}

// use renders the usage line, e.g. "search <query> [limit]".
func (c *command) use() string {
	parts := []string{c.path[len(c.path)-1]}
	for _, p := range c.input.args {
		s := p.name
		if p.typ.Kind() == reflect.Slice {
			s += "..."
		}
		if p.required {
			s = "<" + s + ">"
		} else {
			s = "[" + s + "]"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func (c *command) validateArgs(cc *cobra.Command, args []string) error {
	min, max := 0, len(c.input.args)
	for _, p := range c.input.args {
		if p.required && p.env == "" {
			min++
		}
	}
	if n := len(c.input.args); n > 0 && c.input.args[n-1].typ.Kind() == reflect.Slice {
		max = -1
	}
	switch {
	case len(args) < min:
		return missing(c.input.args[len(args)]).WithHint(helpHint(cc))
	case max >= 0 && len(args) > max:
		return Usage("TOO_MANY_ARGUMENTS", "%s accepts at most %d argument(s), got %d", c.name(), max, len(args)).
			WithHint(helpHint(cc))
	}
	return nil
}

func (a *App) execute(ctx context.Context, c *command, args []string) error {
	raw := rawInput{}
	for i, p := range c.input.args {
		if i >= len(args) {
			break
		}
		if p.typ.Kind() == reflect.Slice {
			raw[p.name] = args[i:]
		} else {
			raw[p.name] = []string{args[i]}
		}
	}
	for name, rv := range c.raw {
		if len(rv.vals) > 0 {
			raw[name] = rv.vals
		}
	}
	in, err := c.input.bind(raw)
	if err != nil {
		e := asError(err)
		if e.Hint == "" {
			e = e.WithHint(helpHint(c.cobra))
		}
		return e
	}
	if c.risk == RiskDestructive && !c.yes && !c.dryRun {
		if err := a.confirm(c); err != nil {
			return err
		}
	}
	out, err := c.run(&Context{Context: ctx, dryRun: c.dryRun, via: "cli", stderr: a.stderr}, in)
	if err != nil {
		return asError(err)
	}
	return a.writeOutput(c.outType, out)
}

// confirm asks a human on a terminal; everyone else must pass --yes.
func (a *App) confirm(c *command) error {
	f, err := a.format()
	if err != nil {
		return err
	}
	if !a.stdinTTY || f != formatText {
		hint := "re-run with --yes once the user has approved this action"
		if c.dryRunnable {
			hint += ", or use --dry-run to preview it"
		}
		return (&Error{
			Kind:    KindConfirmationRequired,
			Code:    "CONFIRMATION_REQUIRED",
			Message: fmt.Sprintf("'%s %s' is destructive and needs confirmation", a.name, c.name()),
		}).WithHint(hint)
	}
	fmt.Fprintf(a.stderr, "'%s %s' is destructive. Continue? [y/N] ", a.name, c.name())
	answer, _ := bufio.NewReader(a.stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	}
	return &Error{Kind: KindConfirmationRequired, Code: "ABORTED", Message: "aborted"}
}

// rawValue collects raw flag values; Axon parses them itself so that CLI and
// MCP inputs share one parser.
type rawValue struct {
	typ  string
	vals []string
}

func (r *rawValue) String() string {
	if len(r.vals) == 0 {
		return ""
	}
	return r.vals[len(r.vals)-1]
}

func (r *rawValue) Set(s string) error {
	r.vals = append(r.vals, s)
	return nil
}

func (r *rawValue) Type() string { return r.typ }

func typeName(t reflect.Type) string {
	if t == durationType {
		return "duration"
	}
	switch t.Kind() {
	case reflect.Slice:
		return typeName(t.Elem()) + "s"
	case reflect.Float64:
		return "float"
	case reflect.Int64:
		return "int"
	}
	return t.Kind().String()
}

func (p *param) usage() string {
	s := p.help
	var notes []string
	if len(p.enum) > 0 {
		notes = append(notes, "one of: "+strings.Join(p.enum, ", "))
	}
	if p.env != "" {
		notes = append(notes, "env: "+p.env)
	}
	if p.required && !p.hasDef && !p.positional {
		notes = append(notes, "required")
	}
	if len(notes) > 0 {
		s += " (" + strings.Join(notes, "; ") + ")"
	}
	return strings.TrimSpace(s)
}
