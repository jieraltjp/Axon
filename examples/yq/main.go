// Command yq is a case study: the real yq CLI moved to Axon. Its existing
// Cobra commands are mounted unchanged, and two typed commands make it usable
// by scripts and agents (JSON output, schema, a Skill and MCP tools).
package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jieraltjp/axon"
	yqcmd "github.com/mikefarah/yq/v4/cmd"
	"github.com/mikefarah/yq/v4/pkg/yqlib"
)

type QueryInput struct {
	Expression string `arg:"" help:"yq expression, such as .spec.replicas"`
	File       string `arg:"" help:"File to read (YAML, JSON, XML, CSV, TSV, TOML or properties)"`
	Format     string `short:"p" help:"Input format (default: from the file extension)"`
}

type QueryResult struct {
	Results []any `json:"results"`
}

type SetInput struct {
	Expression string `arg:"" help:"yq update expression, such as '.spec.replicas = 3'"`
	File       string `arg:"" help:"File to update in place"`
	Format     string `short:"p" help:"File format (default: from the file extension)"`
}

type SetResult struct {
	File    string `json:"file"`
	Changed bool   `json:"changed"`
	DryRun  bool   `json:"dry_run"`
	Content string `json:"content"`
}

func main() {
	yqlib.InitExpressionParser()
	app := axon.New("yq", axon.Description("A portable YAML, JSON, XML, CSV and TOML processor"))
	axon.MountRoot(app, yqcmd.New())

	axon.Command(app, "query", "Evaluate an expression against a file and return the results",
		func(ctx *axon.Context, in QueryInput) (QueryResult, error) {
			src, format, err := read(in.File, in.Format)
			if err != nil {
				return QueryResult{}, err
			}
			prefs := yqlib.NewDefaultJsonPreferences()
			prefs.Indent, prefs.ColorsEnabled, prefs.UnwrapScalar = 0, false, false
			out, err := eval(in.Expression, src, format, yqlib.NewJSONEncoder(prefs))
			if err != nil {
				return QueryResult{}, err
			}
			res := QueryResult{Results: []any{}}
			dec := json.NewDecoder(strings.NewReader(out))
			dec.UseNumber()
			for {
				var v any
				if err := dec.Decode(&v); err == io.EOF {
					break
				} else if err != nil {
					return QueryResult{}, axon.Errorf("BAD_RESULT", "cannot read the result as JSON: %v", err)
				}
				res.Results = append(res.Results, v)
			}
			return res, nil
		},
		axon.Example(`yq query .spec.replicas deploy.yaml`, `yq query '.items[] | select(.done) | .title' todo.json`))

	axon.Command(app, "set", "Update a file in place with an expression, keeping its format",
		func(ctx *axon.Context, in SetInput) (SetResult, error) {
			src, format, err := read(in.File, in.Format)
			if err != nil {
				return SetResult{}, err
			}
			out, err := eval(in.Expression, src, format, format.GetConfiguredEncoder())
			if err != nil {
				return SetResult{}, err
			}
			res := SetResult{File: in.File, Changed: out != src, DryRun: ctx.DryRun(), Content: out}
			if res.Changed && !ctx.DryRun() {
				if err := os.WriteFile(in.File, []byte(out), 0o644); err != nil {
					return SetResult{}, axon.Errorf("WRITE_FAILED", "cannot write %s: %v", in.File, err)
				}
			}
			return res, nil
		},
		axon.Write(), axon.SupportsDryRun(),
		axon.Example(`yq set '.spec.replicas = 3' deploy.yaml --dry-run`))

	app.Run()
}

// read returns the file's contents and its format.
func read(path, format string) (string, *yqlib.Format, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, axon.NotFound("FILE_NOT_FOUND", "%s does not exist", path)
	} else if err != nil {
		return "", nil, axon.Errorf("READ_FAILED", "cannot read %s: %v", path, err)
	}
	if format == "" {
		format = yqlib.FormatStringFromFilename(filepath.Base(path))
	}
	f, err := yqlib.FormatFromString(format)
	if err != nil || f.DecoderFactory == nil {
		return "", nil, axon.Usage("UNKNOWN_FORMAT", "unknown input format %q", format).
			WithHint("use yaml, json, xml, csv, tsv, toml or props")
	}
	return string(data), f, nil
}

// eval evaluates expr against every document in src.
func eval(expr, src string, format *yqlib.Format, enc yqlib.Encoder) (string, error) {
	if _, err := yqlib.ExpressionParser.ParseExpression(expr); err != nil {
		return "", axon.Usage("BAD_EXPRESSION", "invalid expression: %v", err).
			WithHint("see https://mikefarah.gitbook.io/yq/ for the expression syntax")
	}
	out, err := yqlib.NewStringEvaluator().EvaluateAll(expr, src, enc, format.DecoderFactory())
	if err != nil {
		return "", axon.Errorf("EVALUATION_FAILED", "%v", err)
	}
	return out, nil
}
