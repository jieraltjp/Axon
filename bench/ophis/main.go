// Command notes (ophis) is the control group's plain Cobra CLI exposed over
// MCP with ophis (github.com/njayp/ophis), the usual way to add MCP to an
// existing Cobra CLI today. Run it with "notes mcp start".
package main

import (
	"os"

	"github.com/jieraltjp/axon/bench/control/cli"
	"github.com/njayp/ophis"
)

func main() {
	root := cli.Root()
	root.AddCommand(ophis.Command(nil))
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
