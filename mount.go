package axon

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// annotationMounted marks a command added by Mount. annotationLegacyRoot
// marks the app's root once MountRoot has given it a legacy root's flags.
const (
	annotationMounted    = "axon:mounted"
	annotationLegacyRoot = "axon:legacy-root"
)

// MountRoot moves a whole Cobra CLI to Axon. It mounts the root's
// subcommands like Mount does, and if the root itself runs something, that
// becomes what the app does when no command is given, with the root's flags.
// The result is a drop-in replacement for the old CLI that gains Axon's
// built-in commands, and typed commands can be added beside it.
//
// The app's root then has the old root's flags instead of Axon's output
// flags; Axon's typed commands keep theirs.
func MountRoot(app *App, root *cobra.Command) {
	Mount(app, root.Commands()...)
	if !root.Runnable() {
		return
	}
	fs := app.root.Flags()
	add := func(f *pflag.Flag) {
		if fs.Lookup(f.Name) == nil {
			fs.AddFlag(f)
		}
	}
	root.PersistentFlags().VisitAll(add)
	root.LocalNonPersistentFlags().VisitAll(add)
	app.root.Annotations = map[string]string{annotationLegacyRoot: "true"}
	app.root.Args = root.Args
	if app.root.Args == nil {
		app.root.Args = cobra.ArbitraryArgs
	}
	app.root.RunE = func(c *cobra.Command, args []string) error {
		for _, step := range []struct {
			run  func(*cobra.Command, []string)
			runE func(*cobra.Command, []string) error
		}{
			{root.PersistentPreRun, root.PersistentPreRunE},
			{root.PreRun, root.PreRunE},
			{root.Run, root.RunE},
			{root.PostRun, root.PostRunE},
			{root.PersistentPostRun, root.PersistentPostRunE},
		} {
			if step.runE != nil {
				if err := step.runE(c, args); err != nil {
					return asError(err)
				}
			} else if step.run != nil {
				step.run(c, args)
			}
		}
		return nil
	}
}

// Mount adds existing Cobra commands, with their subcommands, to the app so
// that a CLI can move to Axon one command at a time. To keep a whole legacy
// CLI, mount the children of its root: Mount(app, root.Commands()...).
//
// Mounted commands keep their own behavior, flags and output and appear in
// help. If a command came from another root, it keeps what it inherited from
// there: the root's persistent flags and its persistent pre- and post-run
// hooks. Commands named help or completion are skipped, since Axon provides
// both.
//
// Mounted commands are not part of the schema, skills or MCP, because Axon
// knows nothing about their input and output types; rewrite them with Command
// to get those. Errors they return are reported as general errors (exit code
// 1) in Axon's error format; flag and argument mistakes are usage errors
// (exit code 2).
func Mount(app *App, cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		name := cmd.Name()
		if name == "help" || name == "completion" {
			continue
		}
		if reservedTopLevel[name] {
			panic(fmt.Sprintf("axon: cannot mount %q: the name is reserved", name))
		}
		for _, existing := range app.root.Commands() {
			if existing.Name() == name {
				panic(fmt.Sprintf("axon: cannot mount %q: a command with that name exists", name))
			}
		}
		if parent := cmd.Parent(); parent != nil {
			inherit(cmd, parent)
			parent.RemoveCommand(cmd)
		}
		wrapErrors(cmd)
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}
		cmd.Annotations[annotationMounted] = "true"
		cmd.GroupID = groupCommands
		app.root.AddCommand(cmd)
	}
}

// inherit gives cmd what it inherited from its old parents: their persistent
// flags, and the nearest persistent hooks, which Cobra runs only when cmd has
// none of its own.
func inherit(cmd, parent *cobra.Command) {
	for p := parent; p != nil; p = p.Parent() {
		p.PersistentFlags().VisitAll(func(f *pflag.Flag) {
			if cmd.PersistentFlags().Lookup(f.Name) == nil && cmd.LocalFlags().Lookup(f.Name) == nil {
				cmd.PersistentFlags().AddFlag(f)
			}
		})
		if cmd.PersistentPreRun == nil && cmd.PersistentPreRunE == nil {
			cmd.PersistentPreRun, cmd.PersistentPreRunE = p.PersistentPreRun, p.PersistentPreRunE
		}
		if cmd.PersistentPostRun == nil && cmd.PersistentPostRunE == nil {
			cmd.PersistentPostRun, cmd.PersistentPostRunE = p.PersistentPostRun, p.PersistentPostRunE
		}
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
