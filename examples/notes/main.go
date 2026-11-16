// Command notes is a small note-taking CLI built with Axon. It shows one set
// of typed commands served to humans, agents (JSON, schema, skills) and MCP.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jieraltjp/axon"
)

type Note struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Tags    []string  `json:"tags"`
	Body    string    `json:"body,omitempty"`
	Created time.Time `json:"created"`
}

type ListInput struct {
	Tag   string `help:"Only notes with this tag"`
	Limit int    `default:"20" help:"Maximum number of notes"`
}

type SearchInput struct {
	Query string `arg:"" help:"Text to look for in titles and bodies"`
	Limit int    `default:"10" help:"Maximum number of notes"`
}

type IDInput struct {
	ID string `arg:"" help:"Note ID"`
}

type AddInput struct {
	Title string   `arg:"" help:"Note title"`
	Body  string   `help:"Note body"`
	Tags  []string `name:"tag" short:"t" help:"Tag (repeatable)"`
}

type TagCount struct {
	Tag   string `json:"tag"`
	Notes int    `json:"notes"`
}

type Deleted struct {
	ID     string `json:"id"`
	DryRun bool   `json:"dry_run"`
}

func main() {
	app := axon.New("notes",
		axon.Version("0.1.0"),
		axon.Description("Personal markdown notes"))

	axon.Command(app, "list", "List notes, newest first",
		func(ctx *axon.Context, in ListInput) ([]Note, error) {
			notes, err := load()
			if err != nil {
				return nil, err
			}
			var out []Note
			for _, n := range notes {
				if in.Tag == "" || hasTag(n, in.Tag) {
					n.Body = ""
					out = append(out, n)
				}
			}
			return limit(out, in.Limit), nil
		},
		axon.Aliases("ls"),
		axon.Example("notes list --tag go", "notes list --fields id,title"))

	axon.Command(app, "search", "Full-text search notes",
		func(ctx *axon.Context, in SearchInput) ([]Note, error) {
			notes, err := load()
			if err != nil {
				return nil, err
			}
			q := strings.ToLower(in.Query)
			var out []Note
			for _, n := range notes {
				if strings.Contains(strings.ToLower(n.Title+" "+n.Body), q) {
					out = append(out, n)
				}
			}
			return limit(out, in.Limit), nil
		},
		axon.Example("notes search mcp --limit 5"))

	axon.Command(app, "show", "Show a note",
		func(ctx *axon.Context, in IDInput) (Note, error) {
			notes, err := load()
			if err != nil {
				return Note{}, err
			}
			i, err := find(notes, in.ID)
			if err != nil {
				return Note{}, err
			}
			return notes[i], nil
		},
		axon.Example("notes show 1"))

	axon.Command(app, "add", "Create a note",
		func(ctx *axon.Context, in AddInput) (Note, error) {
			notes, err := load()
			if err != nil {
				return Note{}, err
			}
			n := Note{ID: nextID(notes), Title: in.Title, Body: in.Body, Tags: in.Tags, Created: time.Now().UTC()}
			if n.Tags == nil {
				n.Tags = []string{}
			}
			return n, save(append(notes, n))
		},
		axon.Write(),
		axon.Example(`notes add "Ship v0.1" --tag axon --body "Write the README"`))

	axon.Command(app, "delete", "Delete a note",
		func(ctx *axon.Context, in IDInput) (Deleted, error) {
			notes, err := load()
			if err != nil {
				return Deleted{}, err
			}
			i, err := find(notes, in.ID)
			if err != nil {
				return Deleted{}, err
			}
			if ctx.DryRun() {
				return Deleted{ID: in.ID, DryRun: true}, nil
			}
			return Deleted{ID: in.ID}, save(append(notes[:i], notes[i+1:]...))
		},
		axon.Destructive(),
		axon.SupportsDryRun(),
		axon.Aliases("rm"),
		axon.Example("notes delete 3 --dry-run", "notes delete 3 --yes"))

	axon.Group(app, "tag", "Work with tags")
	axon.Command(app, "tag list", "List tags with note counts",
		func(ctx *axon.Context, in struct{}) ([]TagCount, error) {
			notes, err := load()
			if err != nil {
				return nil, err
			}
			counts := map[string]int{}
			for _, n := range notes {
				for _, t := range n.Tags {
					counts[t]++
				}
			}
			var out []TagCount
			for t, c := range counts {
				out = append(out, TagCount{Tag: t, Notes: c})
			}
			sort.Slice(out, func(i, j int) bool {
				return out[i].Notes > out[j].Notes || out[i].Notes == out[j].Notes && out[i].Tag < out[j].Tag
			})
			return out, nil
		})

	app.Run()
}

func storePath() string {
	if p := os.Getenv("NOTES_FILE"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "axon-notes", "notes.json")
}

func load() ([]Note, error) {
	b, err := os.ReadFile(storePath())
	if errors.Is(err, os.ErrNotExist) {
		return seed(), nil
	}
	if err != nil {
		return nil, axon.Unavailable("STORE_UNREADABLE", "cannot read notes: %v", err)
	}
	var notes []Note
	if err := json.Unmarshal(b, &notes); err != nil {
		return nil, axon.Errorf("STORE_CORRUPT", "cannot parse %s: %v", storePath(), err)
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Created.After(notes[j].Created) })
	return notes, nil
}

func save(notes []Note) error {
	path := storePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func seed() []Note {
	t := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	return []Note{
		{ID: "3", Title: "Agent-first CLI checklist", Tags: []string{"agents", "cli"}, Body: "JSON when piped, schema command, structured errors, --fields, confirmations.", Created: t},
		{ID: "2", Title: "MCP vs CLI token costs", Tags: []string{"agents", "mcp"}, Body: "CLI plus skills is usually cheaper than loading every MCP tool schema.", Created: t.Add(-24 * time.Hour)},
		{ID: "1", Title: "Learn Go generics", Tags: []string{"go"}, Body: "Type parameters, constraints, and reflect.TypeFor.", Created: t.Add(-48 * time.Hour)},
	}
}

func find(notes []Note, id string) (int, error) {
	for i, n := range notes {
		if n.ID == id {
			return i, nil
		}
	}
	return 0, axon.NotFound("NOTE_NOT_FOUND", "note %s not found", id).
		WithHint("run 'notes list' to see note IDs")
}

func nextID(notes []Note) string {
	max := 0
	for _, n := range notes {
		var id int
		fmt.Sscan(n.ID, &id)
		if id > max {
			max = id
		}
	}
	return fmt.Sprint(max + 1)
}

func hasTag(n Note, tag string) bool {
	for _, t := range n.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func limit(notes []Note, n int) []Note {
	if n > 0 && len(notes) > n {
		return notes[:n]
	}
	return notes
}
