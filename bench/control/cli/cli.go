// Package cli is the benchmark's control group: the same notes CLI
// written the way most Cobra CLIs are written today. It prints text for
// humans, reports errors as text with exit code 1, and asks before deleting
// unless --force is given. It shares the store format with the Axon example.
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

type Note struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Tags    []string  `json:"tags"`
	Body    string    `json:"body,omitempty"`
	Created time.Time `json:"created"`
}

// Root returns the root command.
func Root() *cobra.Command {
	root := &cobra.Command{Use: "notes", Short: "Personal markdown notes", Version: "0.1.0"}

	var tag string
	var limit int
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List notes, newest first",
		RunE: func(cmd *cobra.Command, args []string) error {
			notes, err := load()
			if err != nil {
				return err
			}
			var out []Note
			for _, n := range notes {
				if tag == "" || contains(n.Tags, tag) {
					out = append(out, n)
				}
			}
			printTable(head(out, limit), false)
			return nil
		},
	}
	list.Flags().StringVar(&tag, "tag", "", "only notes with this tag")
	list.Flags().IntVar(&limit, "limit", 20, "maximum number of notes")

	var searchLimit int
	search := &cobra.Command{
		Use:   "search QUERY",
		Short: "Full-text search notes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			notes, err := load()
			if err != nil {
				return err
			}
			q := strings.ToLower(args[0])
			var out []Note
			for _, n := range notes {
				if strings.Contains(strings.ToLower(n.Title+" "+n.Body), q) {
					out = append(out, n)
				}
			}
			printTable(head(out, searchLimit), false)
			return nil
		},
	}
	search.Flags().IntVar(&searchLimit, "limit", 10, "maximum number of notes")

	show := &cobra.Command{
		Use:   "show ID",
		Short: "Show a note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			notes, err := load()
			if err != nil {
				return err
			}
			i, err := find(notes, args[0])
			if err != nil {
				return err
			}
			n := notes[i]
			fmt.Printf("%s\nTags: %s\nCreated: %s\n\n%s\n", n.Title, strings.Join(n.Tags, ", "), n.Created.Format("Jan 2, 2006"), n.Body)
			return nil
		},
	}

	var body string
	var tags []string
	add := &cobra.Command{
		Use:   "add TITLE",
		Short: "Create a note",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			notes, err := load()
			if err != nil {
				return err
			}
			if tags == nil {
				tags = []string{}
			}
			n := Note{ID: nextID(notes), Title: args[0], Body: body, Tags: tags, Created: time.Now().UTC()}
			if err := save(append(notes, n)); err != nil {
				return err
			}
			fmt.Printf("Created note %s\n", n.ID)
			return nil
		},
	}
	add.Flags().StringVar(&body, "body", "", "note body")
	add.Flags().StringSliceVarP(&tags, "tag", "t", nil, "tags")

	var force bool
	del := &cobra.Command{
		Use:     "delete ID",
		Aliases: []string{"rm"},
		Short:   "Delete a note",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			notes, err := load()
			if err != nil {
				return err
			}
			i, err := find(notes, args[0])
			if err != nil {
				return err
			}
			if !force {
				fmt.Printf("Delete note %s %q? [y/N]: ", notes[i].ID, notes[i].Title)
				answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
					return errors.New("aborted")
				}
			}
			if err := save(append(notes[:i], notes[i+1:]...)); err != nil {
				return err
			}
			fmt.Printf("Deleted note %s\n", args[0])
			return nil
		},
	}
	del.Flags().BoolVarP(&force, "force", "f", false, "delete without asking")

	tagCmd := &cobra.Command{Use: "tag", Short: "Work with tags"}
	tagCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List tags with note counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			notes, err := load()
			if err != nil {
				return err
			}
			counts := map[string]int{}
			for _, n := range notes {
				for _, t := range n.Tags {
					counts[t]++
				}
			}
			keys := make([]string, 0, len(counts))
			for k := range counts {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool {
				return counts[keys[i]] > counts[keys[j]] || counts[keys[i]] == counts[keys[j]] && keys[i] < keys[j]
			})
			for _, k := range keys {
				fmt.Printf("%s (%d)\n", k, counts[k])
			}
			return nil
		},
	})

	root.AddCommand(list, search, show, add, del, tagCmd)
	return root
}

func printTable(notes []Note, withBody bool) {
	if len(notes) == 0 {
		fmt.Println("No notes found.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tTAGS\tCREATED")
	for _, n := range notes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", n.ID, n.Title, strings.Join(n.Tags, ","), n.Created.Format("Jan 2"))
	}
	tw.Flush()
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
		return nil, err
	}
	var notes []Note
	if err := json.Unmarshal(b, &notes); err != nil {
		return nil, err
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Created.After(notes[j].Created) })
	return notes, nil
}

func save(notes []Note) error {
	if err := os.MkdirAll(filepath.Dir(storePath()), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(storePath(), b, 0o644)
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
	return 0, fmt.Errorf("note %s not found", id)
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

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func head(notes []Note, n int) []Note {
	if n > 0 && len(notes) > n {
		return notes[:n]
	}
	return notes
}
