package axon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

type format string

const (
	formatText   format = "text"
	formatJSON   format = "json"
	formatNDJSON format = "ndjson"
)

// format resolves the output format: --output, then $AXON_OUTPUT, then text
// on a terminal and JSON everywhere else.
func (a *App) format() (format, error) {
	name := a.output
	if name == "" {
		name = os.Getenv("AXON_OUTPUT")
	}
	switch format(name) {
	case formatText, formatJSON, formatNDJSON:
		return format(name), nil
	case "":
		if a.stdoutTTY {
			return formatText, nil
		}
		return formatJSON, nil
	}
	return "", Usage("INVALID_OUTPUT", "unknown output format %q", name).
		WithHint("use one of: text, json, ndjson")
}

func (a *App) writeOutput(outType reflect.Type, out any) error {
	f, err := a.format()
	if err != nil {
		return err
	}
	if rv := reflect.ValueOf(out); rv.Kind() == reflect.Slice && rv.IsNil() {
		out = reflect.MakeSlice(rv.Type(), 0, 0).Interface()
	}
	// Encoding the original value keeps struct field order; the generic tree
	// is only needed for --fields and for text rendering.
	data := out
	columns := columnOrder(outType)
	if a.fields != "" || f == formatText {
		if data, err = toJSONValue(out); err != nil {
			return Errorf("ENCODE_FAILED", "cannot encode output: %v", err)
		}
	}
	if a.fields != "" {
		columns = splitList(a.fields)
		if data, err = selectFields(data, columns); err != nil {
			return err
		}
	}
	if a.envelope {
		return a.writeJSON(map[string]any{"ok": true, "data": data})
	}
	switch f {
	case formatJSON:
		return a.writeJSON(data)
	case formatNDJSON:
		for _, item := range items(data) {
			if err := writeCompactJSON(a.stdout, item); err != nil {
				return err
			}
		}
		return nil
	}
	if outType == reflect.TypeFor[Empty]() {
		return nil
	}
	renderText(a.stdout, a.stderr, data, columns)
	return nil
}

// items returns the elements of a slice, or the value itself otherwise.
func items(v any) []any {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return []any{v}
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

// writeJSON writes indented JSON on a terminal and compact JSON otherwise.
func (a *App) writeJSON(v any) error {
	if !a.stdoutTTY {
		return writeCompactJSON(a.stdout, v)
	}
	enc := json.NewEncoder(a.stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeCompactJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func (a *App) writeError(e *Error) {
	f, err := a.format()
	if err != nil {
		f = formatText
		if !a.stdoutTTY {
			f = formatJSON
		}
	}
	switch {
	case a.envelope:
		_ = a.writeJSON(map[string]any{"ok": false, "error": e})
	case f != formatText:
		_ = writeCompactJSON(a.stderr, map[string]any{"error": e})
	default:
		fmt.Fprintf(a.stderr, "Error: %s\n", e.Message)
		if e.Hint != "" {
			fmt.Fprintf(a.stderr, "Hint: %s\n", e.Hint)
		}
	}
}

// toJSONValue round-trips v through JSON so every renderer works on the same
// generic tree. Nil slices become empty arrays.
func toJSONValue(v any) (any, error) {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Slice && rv.IsNil() {
		return []any{}, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	err = dec.Decode(&out)
	return out, err
}

// selectFields keeps only the named keys of an object or of each object in an
// array. Naming a field that does not exist is a usage error.
func selectFields(data any, fields []string) (any, error) {
	available := map[string]bool{}
	pick := func(obj map[string]any) orderedObject {
		out := orderedObject{values: map[string]any{}}
		for k := range obj {
			available[k] = true
		}
		for _, f := range fields {
			if v, ok := obj[f]; ok {
				out.keys = append(out.keys, f)
				out.values[f] = v
			}
		}
		return out
	}
	var result any
	switch d := data.(type) {
	case map[string]any:
		result = pick(d)
	case []any:
		items := make([]any, len(d))
		for i, item := range d {
			if obj, ok := item.(map[string]any); ok {
				items[i] = pick(obj)
			} else {
				items[i] = item
			}
		}
		result = items
	default:
		return nil, Usage("FIELDS_UNSUPPORTED", "--fields only applies to objects and arrays of objects")
	}
	if len(available) > 0 {
		for _, f := range fields {
			if !available[f] {
				return nil, Usage("UNKNOWN_FIELD", "unknown field %q", f).
					WithHint("available fields: " + strings.Join(sortedKeys(available), ", "))
			}
		}
	}
	return result, nil
}

// orderedObject is a JSON object that keeps its keys in a chosen order.
type orderedObject struct {
	keys   []string
	values map[string]any
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(k)
		val, err := json.Marshal(o.values[k])
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		b.Write(val)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// columnOrder returns JSON field names in declaration order for a struct or a
// slice of structs, so tables follow the order the author chose.
func columnOrder(t reflect.Type) []string {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t == timeType {
		return nil
	}
	var names []string
	for _, f := range jsonFields(t) {
		names = append(names, f.name)
	}
	return names
}

// renderText prints data for humans: arrays of objects as tables, objects as
// key/value lines, scalars as-is.
func renderText(out, errOut io.Writer, data any, columns []string) {
	data = plain(data)
	switch d := data.(type) {
	case nil:
	case []any:
		if len(d) == 0 {
			fmt.Fprintln(errOut, "No results.")
			return
		}
		var objs []map[string]any
		for _, item := range d {
			obj, ok := item.(map[string]any)
			if !ok {
				for _, item := range d {
					fmt.Fprintln(out, cell(item, 0))
				}
				return
			}
			objs = append(objs, obj)
		}
		renderTable(out, objs, columns)
	case map[string]any:
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, k := range orderedKeys(d, columns) {
			fmt.Fprintf(tw, "%s:\t%s\n", k, cell(d[k], 0))
		}
		tw.Flush()
	default:
		fmt.Fprintln(out, cell(d, 0))
	}
}

// plain converts ordered objects back into maps for rendering.
func plain(v any) any {
	switch x := v.(type) {
	case orderedObject:
		return x.values
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = plain(item)
		}
		return out
	}
	return v
}

const maxCellWidth = 48

func renderTable(out io.Writer, rows []map[string]any, columns []string) {
	keys := map[string]bool{}
	for _, row := range rows {
		for k := range row {
			keys[k] = true
		}
	}
	cols := orderedKeys(keys, columns)
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = strings.ToUpper(c)
	}
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, row := range rows {
		vals := make([]string, len(cols))
		for i, c := range cols {
			vals[i] = cell(row[c], maxCellWidth)
		}
		fmt.Fprintln(tw, strings.Join(vals, "\t"))
	}
	tw.Flush()
}

// cell formats a value for display, truncating to width runes when width > 0.
func cell(v any, width int) string {
	var s string
	switch x := v.(type) {
	case nil:
		s = ""
	case string:
		s = x
	case json.Number:
		s = x.String()
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = cell(item, 0)
		}
		s = strings.Join(parts, ", ")
	case map[string]any:
		b, _ := json.Marshal(x)
		s = string(b)
	default:
		s = fmt.Sprint(x)
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if width > 0 && utf8.RuneCountInString(s) > width {
		s = string([]rune(s)[:width-1]) + "…"
	}
	return s
}

// orderedKeys returns preferred keys first (if present), then the rest sorted.
func orderedKeys[V any](m map[string]V, preferred []string) []string {
	seen := map[string]bool{}
	var keys []string
	for _, k := range preferred {
		if _, ok := m[k]; ok && !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func sortedKeys[V any](m map[string]V) []string { return orderedKeys(m, nil) }

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
