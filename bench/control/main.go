// Command notes (control) is the benchmark's control group. See package cli.
package main

import (
	"os"

	"github.com/jieraltjp/axon/bench/control/cli"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		os.Exit(1)
	}
}
