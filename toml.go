package axon

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
)

// Minimal editing of Codex's config.toml: Axon only ever adds, replaces or
// removes its own [mcp_servers.<name>] table and leaves every other byte of
// the file alone, so no TOML library is needed.

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(name string) string {
	if bareKey.MatchString(name) {
		return name
	}
	return tomlString(name)
}

// tomlString encodes s as a TOML basic string. JSON string escapes are a
// subset of TOML's.
func tomlString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func tomlServerTable(name, command string, args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = tomlString(a)
	}
	return "[mcp_servers." + tomlKey(name) + "]\n" +
		"command = " + tomlString(command) + "\n" +
		"args = [" + strings.Join(quoted, ", ") + "]\n"
}

// tomlTable locates the [mcp_servers.<name>] table in lines. main ends where
// the table's own keys end; all also covers its subtables such as
// [mcp_servers.<name>.env].
func tomlTable(lines []string, name string) (start, main, all int, ok bool) {
	prefixes := []string{"mcp_servers." + name, `mcp_servers."` + name + `"`}
	header := func(line string) (string, bool) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[[") {
			if end := strings.Index(t, "]"); end > 0 {
				return strings.TrimSpace(t[1:end]), true
			}
		}
		return "", false
	}
	matches := func(h string, sub bool) bool {
		for _, p := range prefixes {
			if (!sub && h == p) || (sub && strings.HasPrefix(h, p+".")) {
				return true
			}
		}
		return false
	}
	start = -1
	for i, line := range lines {
		if h, isHeader := header(line); isHeader && matches(h, false) {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, 0, false
	}
	main, all = len(lines), len(lines)
	for i := start + 1; i < len(lines); i++ {
		h, isHeader := header(lines[i])
		if !isHeader {
			continue
		}
		if main == len(lines) {
			main = i
		}
		if !matches(h, true) {
			all = i
			break
		}
	}
	return start, main, all, true
}

// readText reads a file; a missing file is empty. It reports whether the file
// existed and whether it uses CRLF line endings.
func readText(path string) (lines []string, existed, crlf bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, Errorf("READ_FAILED", "cannot read %s: %v", path, err)
	}
	text := string(b)
	crlf = strings.Contains(text, "\r\n")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return nil, true, crlf, nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), true, crlf, nil
}

func joinText(lines []string, crlf bool) []byte {
	if len(lines) == 0 {
		return nil
	}
	text := strings.Join(lines, "\n") + "\n"
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return []byte(text)
}

// trimBlank drops trailing blank lines.
func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// tomlUpsert adds or replaces the server table. It reports "installed",
// "updated" or "unchanged" and returns the new file contents.
func tomlUpsert(lines []string, name, command string, args []string) ([]string, string) {
	table := strings.Split(strings.TrimSuffix(tomlServerTable(name, command, args), "\n"), "\n")
	start, main, _, ok := tomlTable(lines, name)
	if !ok {
		out := trimBlank(append([]string{}, lines...))
		if len(out) > 0 {
			out = append(out, "")
		}
		return append(out, table...), "installed"
	}
	current := trimBlank(append([]string{}, lines[start:main]...))
	if strings.Join(current, "\n") == strings.Join(table, "\n") {
		return lines, "unchanged"
	}
	// Replace the table's own keys; keep its subtables and the blank line
	// that separates it from what follows.
	out := append([]string{}, lines[:start]...)
	out = append(out, table...)
	if len(current) < main-start {
		out = append(out, "")
	}
	return append(out, lines[main:]...), "updated"
}

// tomlRemove removes the server table and its subtables.
func tomlRemove(lines []string, name string) ([]string, bool) {
	start, _, all, ok := tomlTable(lines, name)
	if !ok {
		return lines, false
	}
	out := trimBlank(append([]string{}, lines[:start]...))
	rest := lines[all:]
	if len(out) > 0 && len(rest) > 0 {
		out = append(out, "")
	}
	return trimBlank(append(out, rest...)), true
}
