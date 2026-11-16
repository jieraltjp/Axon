package axon

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func legacyCommand() *cobra.Command {
	var loud bool
	legacy := &cobra.Command{Use: "legacy", Short: "An existing Cobra command"}
	hello := &cobra.Command{
		Use:  "hello NAME",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if args[0] == "fail" {
				return errors.New("legacy failure")
			}
			msg := "hello " + args[0]
			if loud {
				msg = strings.ToUpper(msg)
			}
			fmt.Fprintln(c.OutOrStdout(), msg)
			return nil
		},
	}
	hello.Flags().BoolVar(&loud, "loud", false, "shout")
	legacy.AddCommand(hello)
	return legacy
}

func TestMount(t *testing.T) {
	h := newHarness(t, false, "")
	root := &cobra.Command{Use: "old"}
	root.AddCommand(legacyCommand())
	Mount(h.app, root.Commands()...)

	if code := h.run("legacy", "hello", "axon", "--loud"); code != ExitOK || h.stdout.String() != "HELLO AXON\n" {
		t.Errorf("mounted command: exit %d, out %q, err %q", code, h.stdout, h.stderr)
	}
	if code := h.run("legacy", "hello", "axon"); code != ExitOK || h.stdout.String() != "hello axon\n" {
		t.Errorf("flags must reset between runs: exit %d, out %q", code, h.stdout)
	}
	if code := h.run("legacy", "hello", "fail"); code != ExitError || h.errorJSON(t).Message != "legacy failure" {
		t.Errorf("runtime error: exit %d, err %q", code, h.stderr)
	}
	if code := h.run("legacy", "hello"); code != ExitUsage {
		t.Errorf("argument error: exit %d, want %d", code, ExitUsage)
	}

	h.run("--help")
	if !strings.Contains(h.stdout.String(), "legacy") {
		t.Error("mounted command missing from help")
	}
	for _, c := range h.app.Manifest().Commands {
		if strings.HasPrefix(c.Name, "legacy") {
			t.Errorf("mounted command %q must not appear in the manifest", c.Name)
		}
	}
}

func TestMountConflicts(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"reserved": {Use: "schema"},
		"existing": {Use: "find"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic")
				}
			}()
			Mount(newHarness(t, false, "").app, cmd)
		})
	}
}

// A typical Cobra CLI: setup in the root's persistent hook, persistent flags
// on the root (including -o, which Axon also uses), and its own completion.
func TestMountKeepsWhatTheRootProvided(t *testing.T) {
	h := newHarness(t, false, "")
	var format, prefix string
	root := &cobra.Command{
		Use: "old",
		PersistentPreRunE: func(*cobra.Command, []string) error {
			prefix = "ready:"
			return nil
		},
	}
	root.PersistentFlags().StringVarP(&format, "output-format", "o", "yaml", "output format")
	root.AddCommand(&cobra.Command{Use: "completion"})
	root.AddCommand(&cobra.Command{
		Use: "show",
		RunE: func(c *cobra.Command, _ []string) error {
			fmt.Fprintln(c.OutOrStdout(), prefix+format)
			return nil
		},
	})
	Mount(h.app, root.Commands()...)

	if code := h.run("show", "-o", "json"); code != ExitOK || h.stdout.String() != "ready:json\n" {
		t.Errorf("show -o json: exit %d, out %q, err %q", code, h.stdout, h.stderr)
	}
	if code := h.run("show"); code != ExitOK || h.stdout.String() != "ready:yaml\n" {
		t.Errorf("show: exit %d, out %q, err %q", code, h.stdout, h.stderr)
	}
	// Axon's own commands keep their -o.
	if code := h.run("find", "x", "-o", "text"); code != ExitOK || strings.HasPrefix(h.stdout.String(), "[") {
		t.Errorf("find -o text: exit %d, out %q, err %q", code, h.stdout, h.stderr)
	}
	if code := h.run("-o", "text", "find", "x"); code != ExitOK || strings.HasPrefix(h.stdout.String(), "[") {
		t.Errorf("-o text find: exit %d, out %q, err %q", code, h.stdout, h.stderr)
	}
}

func TestMountRoot(t *testing.T) {
	h := newHarness(t, false, "")
	var format string
	var ran []string
	root := &cobra.Command{
		Use:               "old",
		PersistentPreRun:  func(*cobra.Command, []string) { ran = append(ran, "setup") },
		PersistentPostRun: func(*cobra.Command, []string) { ran = append(ran, "teardown") },
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 1 && args[0] == "oops" {
				return errors.New("root failure")
			}
			ran = append(ran, "run")
			fmt.Fprintln(c.OutOrStdout(), format, strings.Join(args, ","))
			return nil
		},
	}
	root.PersistentFlags().StringVarP(&format, "output-format", "o", "yaml", "output format")
	root.AddCommand(legacyCommand())
	MountRoot(h.app, root)

	if code := h.run("-o", "json", ".a", "file"); code != ExitOK || h.stdout.String() != "json .a,file\n" {
		t.Errorf("root action: exit %d, out %q, err %q", code, h.stdout, h.stderr)
	}
	if got := strings.Join(ran, " "); got != "setup run teardown" {
		t.Errorf("hooks ran as %q", got)
	}
	if code := h.run("oops"); code != ExitError || h.errorJSON(t).Message != "root failure" {
		t.Errorf("root failure: exit %d, err %q", code, h.stderr)
	}
	if code := h.run("legacy", "hello", "axon"); code != ExitOK || h.stdout.String() != "hello axon\n" {
		t.Errorf("subcommand: exit %d, out %q", code, h.stdout)
	}
	if code := h.run("find", "x", "-o", "text"); code != ExitOK || strings.HasPrefix(h.stdout.String(), "[") {
		t.Errorf("typed command -o text: exit %d, out %q, err %q", code, h.stdout, h.stderr)
	}
	if code := h.run("schema"); code != ExitOK {
		t.Errorf("schema: exit %d, err %q", code, h.stderr)
	}
}
