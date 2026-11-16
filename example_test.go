package axon_test

import (
	"fmt"
	"strings"

	"github.com/jieraltjp/axon"
)

type GreetInput struct {
	Name  string `arg:"" help:"Who to greet"`
	Shout bool   `help:"Use capitals"`
}

type Greeting struct {
	Message string `json:"message"`
}

func greet(ctx *axon.Context, in GreetInput) (Greeting, error) {
	msg := "Hello, " + in.Name
	if in.Shout {
		msg = strings.ToUpper(msg) + "!"
	}
	return Greeting{Message: msg}, nil
}

// A command is a typed Go function. When stdout is not a terminal, as here,
// the result is printed as JSON.
func Example() {
	app := axon.New("hello", axon.Description("Says hello"))
	axon.Command(app, "greet", "Greet someone", greet)

	app.Execute([]string{"greet", "Axon", "--shout"})
	// Output:
	// {"message":"HELLO, AXON!"}
}

// Destructive commands refuse to run without --yes when no human can be
// asked, and exit with code 4. --dry-run previews the action.
func ExampleDestructive() {
	type DeleteInput struct {
		ID string `arg:""`
	}
	app := axon.New("notes")
	axon.Command(app, "delete", "Delete a note",
		func(ctx *axon.Context, in DeleteInput) (map[string]any, error) {
			return map[string]any{"deleted": in.ID, "dry_run": ctx.DryRun()}, nil
		},
		axon.Destructive(), axon.SupportsDryRun())

	fmt.Println("exit code:", app.Execute([]string{"delete", "42"}))
	app.Execute([]string{"delete", "42", "--dry-run"})
	app.Execute([]string{"delete", "42", "--yes"})
	// Output:
	// exit code: 4
	// {"deleted":"42","dry_run":true}
	// {"deleted":"42","dry_run":false}
}

// Returning a structured error controls the error kind, code, hint and exit
// code that humans and agents see.
func ExampleNotFound() {
	type ShowInput struct {
		ID string `arg:""`
	}
	app := axon.New("notes")
	axon.Command(app, "show", "Show a note",
		func(ctx *axon.Context, in ShowInput) (Greeting, error) {
			return Greeting{}, axon.NotFound("NOTE_NOT_FOUND", "note %s not found", in.ID).
				WithHint("run 'notes list' to see note IDs")
		})

	fmt.Println("exit code:", app.Execute([]string{"show", "7"}))
	// Output:
	// exit code: 3
}

// The manifest describes every command for agents and tools; it is what the
// built-in "schema" command prints.
func ExampleApp_Manifest() {
	app := axon.New("hello", axon.Version("1.0.0"))
	axon.Command(app, "greet", "Greet someone", greet)

	m := app.Manifest()
	spec := m.Commands[0]
	fmt.Println(m.Protocol, m.Name, m.Version)
	fmt.Println(spec.Usage)
	fmt.Println(spec.Input["required"])
	// Output:
	// axon/v0 hello 1.0.0
	// hello greet <name> [flags]
	// [name]
}

// SkillFiles renders an Agent Skill that tells agents how to call the CLI.
func ExampleApp_SkillFiles() {
	app := axon.New("hello", axon.Description("Says hello"))
	axon.Command(app, "greet", "Greet someone", greet, axon.Example("hello greet Axon"))

	skill := app.SkillFiles()["SKILL.md"]
	fmt.Println(strings.Split(skill, "\n")[1])
	fmt.Println(strings.Contains(skill, "### `hello greet <name> [flags]`"))
	// Output:
	// name: hello
	// true
}
