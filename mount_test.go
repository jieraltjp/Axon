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
