package axon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// param describes one input field, derived from struct tags:
//
//	arg:""          positional argument (in declaration order)
//	name:"x"        override the derived kebab-case name
//	short:"n"       one-letter flag alias
//	help:"..."      description, shown in help, schema, skills and MCP
//	default:"10"    default value
//	required:""     must be provided
//	enum:"a,b,c"    allowed values
//	env:"VAR"       read from the environment when not given explicitly
//	axon:"-"        ignore the field
type param struct {
	name       string
	index      []int
	typ        reflect.Type
	help       string
	def        string
	hasDef     bool
	required   bool
	positional bool
	enum       []string
	env        string
	short      string
}

type inputSpec struct {
	typ   reflect.Type
	flags []*param
	args  []*param
}

func (s *inputSpec) params() []*param {
	return append(append([]*param{}, s.args...), s.flags...)
}

var durationType = reflect.TypeOf(time.Duration(0))

func parseInput(t reflect.Type) (*inputSpec, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input type %s must be a struct", t)
	}
	spec := &inputSpec{typ: t}
	seen := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() || f.Tag.Get("axon") == "-" {
			continue
		}
		if !supportedType(f.Type) {
			return nil, fmt.Errorf("field %s: unsupported type %s", f.Name, f.Type)
		}
		p := &param{
			name:  f.Tag.Get("name"),
			index: f.Index,
			typ:   f.Type,
			help:  f.Tag.Get("help"),
			env:   f.Tag.Get("env"),
			short: f.Tag.Get("short"),
		}
		if p.name == "" {
			p.name = kebab(f.Name)
		}
		p.def, p.hasDef = f.Tag.Lookup("default")
		_, p.required = f.Tag.Lookup("required")
		_, p.positional = f.Tag.Lookup("arg")
		if e := f.Tag.Get("enum"); e != "" {
			p.enum = strings.Split(e, ",")
		}
		if p.hasDef {
			if err := setFromString(reflect.New(p.typ).Elem(), p.def); err != nil {
				return nil, fmt.Errorf("field %s: bad default %q: %v", f.Name, p.def, err)
			}
		}
		if seen[p.name] {
			return nil, fmt.Errorf("field %s: duplicate name %q", f.Name, p.name)
		}
		seen[p.name] = true
		if p.positional {
			if n := len(spec.args); n > 0 && spec.args[n-1].typ.Kind() == reflect.Slice {
				return nil, fmt.Errorf("field %s: a slice argument must be the last positional argument", f.Name)
			}
			// Positional arguments without a default are required.
			if !p.hasDef {
				p.required = true
			}
			spec.args = append(spec.args, p)
		} else {
			spec.flags = append(spec.flags, p)
		}
	}
	return spec, nil
}

func supportedType(t reflect.Type) bool {
	if t == durationType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int64, reflect.Float64:
		return true
	case reflect.Slice:
		k := t.Elem().Kind()
		return k == reflect.String || k == reflect.Int
	}
	return false
}

// kebab converts a Go identifier to kebab-case: "MaxResults" -> "max-results", "ID" -> "id".
func kebab(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if i > 0 && (unicode.IsLower(runes[i-1]) || nextLower) {
				b.WriteByte('-')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// setFromString parses s into v according to v's type. Slices receive one
// element per call, so repeated flags accumulate; commas also separate values.
func setFromString(v reflect.Value, s string) error {
	if v.Type() == durationType {
		d, err := time.ParseDuration(s)
		if err != nil {
			return errors.New("expected a duration such as 30s or 5m")
		}
		v.SetInt(int64(d))
		return nil
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return errors.New("expected true or false")
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return errors.New("expected an integer")
		}
		v.SetInt(n)
	case reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return errors.New("expected a number")
		}
		v.SetFloat(f)
	case reflect.Slice:
		for _, part := range strings.Split(s, ",") {
			elem := reflect.New(v.Type().Elem()).Elem()
			if err := setFromString(elem, strings.TrimSpace(part)); err != nil {
				return err
			}
			v.Set(reflect.Append(v, elem))
		}
	default:
		return fmt.Errorf("unsupported type %s", v.Type())
	}
	return nil
}

// rawInput holds unparsed values for each parameter, in the order received.
type rawInput map[string][]string

// bind builds an input value from raw command-line values.
func (s *inputSpec) bind(raw rawInput) (reflect.Value, error) {
	return s.fill(func(p *param, field reflect.Value) (bool, error) {
		vals, ok := raw[p.name]
		if !ok {
			return false, nil
		}
		for _, val := range vals {
			if err := setFromString(field, val); err != nil {
				return true, invalid(p, val, err)
			}
		}
		return true, nil
	})
}

// bindJSON builds an input value from a JSON object (used by MCP).
func (s *inputSpec) bindJSON(data []byte) (reflect.Value, error) {
	obj := map[string]json.RawMessage{}
	if len(data) > 0 && string(data) != "null" {
		if err := json.Unmarshal(data, &obj); err != nil {
			return reflect.Value{}, Usage("INVALID_ARGUMENTS", "arguments must be a JSON object: %v", err)
		}
	}
	known := map[string]bool{}
	for _, p := range s.params() {
		known[p.name] = true
	}
	for name := range obj {
		if !known[name] {
			return reflect.Value{}, Usage("UNKNOWN_ARGUMENT", "unknown argument %q", name)
		}
	}
	return s.fill(func(p *param, field reflect.Value) (bool, error) {
		msg, ok := obj[p.name]
		if !ok {
			return false, nil
		}
		if err := unmarshalParam(msg, field.Addr().Interface(), p); err != nil {
			return true, invalid(p, string(msg), err)
		}
		return true, nil
	})
}

// fill builds an input value. explicit sets a field from the caller's values
// and reports whether it did; otherwise the environment variable and then the
// default apply.
func (s *inputSpec) fill(explicit func(*param, reflect.Value) (bool, error)) (reflect.Value, error) {
	v := reflect.New(s.typ).Elem()
	for _, p := range s.params() {
		field := v.FieldByIndex(p.index)
		ok, err := explicit(p, field)
		if err != nil {
			return v, err
		}
		if ok {
			continue
		}
		if p.env != "" {
			if ev, found := os.LookupEnv(p.env); found {
				if err := setFromString(field, ev); err != nil {
					return v, invalid(p, ev, err)
				}
				continue
			}
		}
		switch {
		case p.hasDef:
			if err := setFromString(field, p.def); err != nil {
				return v, invalid(p, p.def, err)
			}
		case p.required:
			return v, missing(p)
		}
	}
	return v, s.validate(v)
}

func unmarshalParam(msg json.RawMessage, dst any, p *param) error {
	if p.typ == durationType {
		var s string
		if err := json.Unmarshal(msg, &s); err != nil {
			return fmt.Errorf("expected a duration string such as \"30s\"")
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		*(dst.(*time.Duration)) = d
		return nil
	}
	return json.Unmarshal(msg, dst)
}

func (s *inputSpec) validate(v reflect.Value) error {
	for _, p := range s.params() {
		if len(p.enum) == 0 {
			continue
		}
		field := v.FieldByIndex(p.index)
		var vals []string
		if field.Kind() == reflect.Slice {
			for i := 0; i < field.Len(); i++ {
				vals = append(vals, fmt.Sprint(field.Index(i).Interface()))
			}
		} else if !field.IsZero() {
			vals = []string{fmt.Sprint(field.Interface())}
		}
		for _, val := range vals {
			if !contains(p.enum, val) {
				return Usage("INVALID_VALUE", "invalid value %q for %s", val, p.display()).
					WithHint("allowed values: " + strings.Join(p.enum, ", "))
			}
		}
	}
	return nil
}

func (p *param) display() string {
	if p.positional {
		return "<" + p.name + ">"
	}
	return "--" + p.name
}

func missing(p *param) *Error {
	return Usage("MISSING_ARGUMENT", "missing required %s", p.display())
}

func invalid(p *param, val string, err error) *Error {
	return Usage("INVALID_VALUE", "invalid value %q for %s: %v", val, p.display(), err)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
