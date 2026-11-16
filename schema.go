package axon

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// Schema is a JSON Schema (draft 2020-12) document.
type Schema = map[string]any

var (
	timeType       = reflect.TypeOf(time.Time{})
	rawMessageType = reflect.TypeOf(json.RawMessage{})
)

// inputSchema describes a command's input as a JSON object schema. Property
// names match the command-line flag names, so CLI and MCP calls look alike.
func (s *inputSpec) inputSchema() Schema {
	props := map[string]any{}
	required := []string{}
	for _, p := range s.params() {
		prop := typeSchema(p.typ, nil)
		if p.typ == durationType {
			prop = Schema{"type": "string", "description": "duration such as 30s or 5m"}
		}
		if p.help != "" {
			prop["description"] = p.help
		}
		if p.hasDef {
			d := reflect.New(p.typ).Elem()
			_ = setFromString(d, p.def) // validated in parseInput
			if p.typ == durationType {
				prop["default"] = p.def
			} else {
				prop["default"] = d.Interface()
			}
		}
		if len(p.enum) > 0 {
			target := prop
			if p.typ.Kind() == reflect.Slice {
				target = prop["items"].(Schema)
			}
			target["enum"] = p.enum
		}
		if p.required && !p.hasDef {
			required = append(required, p.name)
		}
		props[p.name] = prop
	}
	schema := Schema{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// outputSchema describes the JSON a command writes on success.
func outputSchema(t reflect.Type) Schema {
	return typeSchema(t, map[reflect.Type]bool{})
}

// typeSchema derives a schema from a Go type following encoding/json rules.
// Recursive types are cut off with an unconstrained schema.
func typeSchema(t reflect.Type, visiting map[reflect.Type]bool) Schema {
	if visiting == nil {
		visiting = map[reflect.Type]bool{}
	}
	switch t {
	case timeType:
		return Schema{"type": "string", "format": "date-time"}
	case rawMessageType:
		return Schema{}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return typeSchema(t.Elem(), visiting)
	case reflect.String:
		return Schema{"type": "string"}
	case reflect.Bool:
		return Schema{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Schema{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return Schema{"type": "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return Schema{"type": "string", "contentEncoding": "base64"}
		}
		return Schema{"type": "array", "items": typeSchema(t.Elem(), visiting)}
	case reflect.Map:
		return Schema{"type": "object", "additionalProperties": typeSchema(t.Elem(), visiting)}
	case reflect.Struct:
		if visiting[t] {
			return Schema{}
		}
		visiting[t] = true
		defer delete(visiting, t)
		props := map[string]any{}
		required := []string{}
		for _, f := range jsonFields(t) {
			props[f.name] = typeSchema(f.typ, visiting)
			if !f.omitempty {
				required = append(required, f.name)
			}
		}
		s := Schema{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	return Schema{}
}

type jsonField struct {
	name      string
	typ       reflect.Type
	omitempty bool
}

// jsonFields lists a struct's JSON fields in declaration order, flattening
// embedded structs the way encoding/json does (ignoring name conflicts).
func jsonFields(t reflect.Type) []jsonField {
	var fields []jsonField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				fields = append(fields, jsonFields(ft)...)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields = append(fields, jsonField{
			name:      name,
			typ:       f.Type,
			omitempty: strings.Contains(opts, "omitempty") || strings.Contains(opts, "omitzero"),
		})
	}
	return fields
}
