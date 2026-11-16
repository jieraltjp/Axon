// Command notes (large) is the notes example plus 75 unrelated commands, for
// measuring how the interfaces behave when a CLI has many commands.
package main

import (
	"fmt"

	"github.com/jieraltjp/axon"
	"github.com/jieraltjp/axon/examples/notes/notesapp"
)

var resources = []string{
	"project", "user", "team", "billing", "invoice", "deploy", "env", "secret",
	"webhook", "report", "audit", "backup", "export", "import", "config",
}

type GetInput struct {
	ID string `arg:"" help:"Resource ID"`
}

type ListInput struct {
	Filter string `help:"Filter expression"`
	Limit  int    `default:"50" help:"Maximum number of results"`
	Sort   string `enum:"name,created,updated" default:"created" help:"Sort key"`
}

type CreateInput struct {
	Name        string   `arg:"" help:"Name of the new resource"`
	Description string   `help:"Free-form description"`
	Labels      []string `name:"label" help:"Label in key=value form (repeatable)"`
}

type UpdateInput struct {
	ID          string `arg:"" help:"Resource ID"`
	Name        string `help:"New name"`
	Description string `help:"New description"`
}

type Resource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func main() {
	app := axon.New("notes",
		axon.Version("0.1.0"),
		axon.Description("Personal markdown notes, plus workspace administration"))
	notesapp.Register(app)

	for _, r := range resources {
		r := r
		axon.Group(app, r, fmt.Sprintf("Manage %s resources", r))
		axon.Command(app, r+" list", fmt.Sprintf("List %s resources", r),
			func(ctx *axon.Context, in ListInput) ([]Resource, error) { return []Resource{}, nil })
		axon.Command(app, r+" get", fmt.Sprintf("Show one %s resource", r),
			func(ctx *axon.Context, in GetInput) (Resource, error) {
				return Resource{}, axon.NotFound("NOT_FOUND", "%s %s not found", r, in.ID)
			})
		axon.Command(app, r+" create", fmt.Sprintf("Create a %s resource", r),
			func(ctx *axon.Context, in CreateInput) (Resource, error) {
				return Resource{ID: "1", Name: in.Name, Description: in.Description}, nil
			}, axon.Write())
		axon.Command(app, r+" update", fmt.Sprintf("Update a %s resource", r),
			func(ctx *axon.Context, in UpdateInput) (Resource, error) {
				return Resource{}, axon.NotFound("NOT_FOUND", "%s %s not found", r, in.ID)
			}, axon.Write())
		axon.Command(app, r+" delete", fmt.Sprintf("Delete a %s resource", r),
			func(ctx *axon.Context, in GetInput) (axon.Empty, error) {
				return axon.Empty{}, axon.NotFound("NOT_FOUND", "%s %s not found", r, in.ID)
			}, axon.Destructive())
	}
	app.Run()
}
