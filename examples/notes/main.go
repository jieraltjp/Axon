// Command notes is a small note-taking CLI built with Axon. It shows one set
// of typed commands served to humans, agents (JSON, schema, skills) and MCP.
package main

import (
	"github.com/jieraltjp/axon"
	"github.com/jieraltjp/axon/examples/notes/notesapp"
)

func main() {
	app := axon.New("notes",
		axon.Version("0.1.0"),
		axon.Description("Personal markdown notes"))
	notesapp.Register(app)
	app.Run()
}
