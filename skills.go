package axon

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// skillTokenBudget is the approximate size above which command details move
// from SKILL.md into reference files that agents load on demand.
const skillTokenBudget = 2000

// SkillFiles renders an Agent Skill for the application, keyed by path
// relative to the skill directory ("SKILL.md", "references/tag.md", ...).
func (a *App) SkillFiles() map[string]string {
	var cmds []*command
	for _, c := range a.commands {
		if c.visibleIn(ProjectionSkill) {
			cmds = append(cmds, c)
		}
	}

	var details strings.Builder
	for _, c := range cmds {
		details.WriteString(a.skillCommand(c))
	}
	head := a.skillHead(cmds)
	if (len(head)+details.Len())/4 <= skillTokenBudget {
		return map[string]string{"SKILL.md": head + "## Commands\n\n" + details.String()}
	}

	// Too large: keep an index in SKILL.md and one reference file per
	// top-level command group.
	files := map[string]string{}
	groups := map[string][]*command{}
	for _, c := range cmds {
		groups[c.path[0]] = append(groups[c.path[0]], c)
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)

	var index strings.Builder
	index.WriteString("## Commands\n\nEach group's arguments, flags and examples are in its reference file; read it before using the group.\n\n")
	index.WriteString("| Group | About | Details |\n| --- | --- | --- |\n")
	for _, name := range names {
		ref := "references/" + name + ".md"
		var body strings.Builder
		fmt.Fprintf(&body, "# %s %s\n\n", a.name, name)
		var subs []string
		for _, c := range groups[name] {
			subs = append(subs, strings.Join(c.path[1:], " "))
			body.WriteString(a.skillCommand(c))
		}
		about := groups[name][0].description
		if g, ok := a.groups[name]; ok {
			about = fmt.Sprintf("%s (%s)", strings.TrimSuffix(g.Short, "."), strings.Join(subs, ", "))
		}
		fmt.Fprintf(&index, "| `%s %s` | %s | [%s](%s) |\n", a.name, name, about, ref, ref)
		files[ref] = body.String()
	}
	files["SKILL.md"] = head + index.String()
	return files
}

func (a *App) skillHead(cmds []*command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: %s\n---\n\n", skillName(a.name), yamlString(a.skillDescription(cmds)))
	fmt.Fprintf(&b, "# %s\n\n", a.name)
	if a.description != "" {
		fmt.Fprintf(&b, "%s\n\n", a.description)
	}
	fmt.Fprintf(&b, `## How to use

- Run `+"`%[1]s <command> [args] [flags]`"+`. When stdout is not a terminal the output is JSON (`+"`-o ndjson`"+` for one item per line).
- Keep output small with `+"`--fields a,b`"+`.
- Run `+"`%[1]s schema <command>`"+` for the exact input and output JSON Schema of a command.
- On failure the exit code is non-zero and stderr holds `+"`{\"error\":{\"kind\",\"code\",\"message\",\"hint\"}}`"+`. Follow the hint. Exit codes: 2 usage, 3 not found, 4 confirmation required, 5 permission denied, 6 temporary (retry).
- Commands marked **destructive** refuse to run without `+"`--yes`"+`. Only add `+"`--yes`"+` after the user has explicitly approved that specific action. Prefer `+"`--dry-run`"+` first when the command supports it.

`, a.name)
	return b.String()
}

func (a *App) skillDescription(cmds []*command) string {
	var parts []string
	for _, c := range cmds {
		parts = append(parts, strings.TrimSuffix(c.description, "."))
	}
	desc := strings.TrimSpace(a.description)
	if desc != "" && !strings.HasSuffix(desc, ".") {
		desc += "."
	}
	desc = strings.TrimSpace(fmt.Sprintf("%s Use the `%s` CLI to: %s.", desc, a.name, strings.Join(parts, "; ")))
	if r := []rune(desc); len(r) > 1024 {
		desc = string(r[:1021]) + "..."
	}
	return desc
}

func (a *App) skillCommand(c *command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### `%s`\n\n%s\n\n", a.usage(c), c.description)
	if c.risk != RiskRead {
		fmt.Fprintf(&b, "- Risk: **%s**\n", c.risk)
	}
	for _, p := range c.input.args {
		fmt.Fprintf(&b, "- `<%s>` %s\n", p.name, p.usage())
	}
	for _, p := range c.input.flags {
		line := fmt.Sprintf("- `--%s %s` %s", p.name, typeName(p.typ), p.usage())
		if p.typ.Kind() == reflect.Slice {
			line = fmt.Sprintf("- `--%s %s` %s", p.name, typeName(p.typ.Elem()), p.usage())
			if !strings.Contains(p.help, "repeatable") {
				line += " (repeatable)"
			}
		}
		if p.typ.Kind() == reflect.Bool {
			line = fmt.Sprintf("- `--%s` %s", p.name, p.usage())
		}
		if p.hasDef {
			line += fmt.Sprintf(" (default %s)", p.def)
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	if c.risk == RiskDestructive {
		b.WriteString("- `--yes` Confirm (requires the user's explicit approval)\n")
	}
	if c.dryRunnable {
		b.WriteString("- `--dry-run` Preview without making changes\n")
	}
	for _, ex := range c.examples {
		fmt.Fprintf(&b, "- Example: `%s`\n", ex)
	}
	b.WriteString("\n")
	return b.String()
}

var nonSkillChars = regexp.MustCompile(`[^a-z0-9]+`)

// skillName converts an application name into a valid skill name: lowercase
// letters, digits and hyphens, at most 64 characters.
func skillName(name string) string {
	s := strings.Trim(nonSkillChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 64 {
		s = strings.TrimRight(s[:64], "-")
	}
	if s == "" {
		s = "cli"
	}
	return s
}

// yamlString quotes s for use as a YAML scalar.
func yamlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(s) + `"`
}
