package axon

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Mount adds existing Cobra commands, with their subcommands, to the app so
// that a CLI can move to Axon one command at a time. To keep a whole legacy
// CLI, mount the children of its root: Mount(app, root.Commands()...).
//
// Mounted commands keep their own behavior and output and appear in help.
// They are not part of the schema, skills or MCP, because Axon knows nothing
// about their input and output types; rewrite them with Command to get those.
// Errors they return are reported as general errors (exit code 1) in Axon's
// error format; flag and argument mistakes are usage errors (exit code 2).
func Mount(app *App, cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		name := cmd.Name()
		if reservedTopLevel[name] {
			panic(fmt.Sprintf("axon: cannot mount %q: the name is reserved", name))
		}
		for _, existing := range app.root.Commands() {
			if existing.Name() == name {
				panic(fmt.Sprintf("axon: cannot mount %q: a command with that name exists", name))
			}
		}
		wrapErrors(cmd)
		cmd.GroupID = groupCommands
		app.root.AddCommand(cmd)
	}
}

// wrapErrors makes runtime errors from cmd and its subcommands general errors
// rather than usage errors.
func wrapErrors(cmd *cobra.Command) {
	wrap := func(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
		if run == nil {
			return nil
		}
		return func(c *cobra.Command, args []string) error {
			if err := run(c, args); err != nil {
				return asError(err)
			}
			return nil
		}
	}
	cmd.RunE = wrap(cmd.RunE)
	cmd.PreRunE = wrap(cmd.PreRunE)
	cmd.PostRunE = wrap(cmd.PostRunE)
	cmd.PersistentPreRunE = wrap(cmd.PersistentPreRunE)
	cmd.PersistentPostRunE = wrap(cmd.PersistentPostRunE)
	for _, sub := range cmd.Commands() {
		wrapErrors(sub)
	}
}
