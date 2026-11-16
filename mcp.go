package axon

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPOptions configures the MCP projection.
type MCPOptions struct {
	// AllowDestructive exposes destructive commands as tools. They are hidden
	// by default; when exposed, the MCP client is responsible for asking the
	// user, guided by the tool's destructiveHint annotation.
	AllowDestructive bool
}

// MCPServer returns an MCP server exposing the application's commands as tools.
func (a *App) MCPServer(opts MCPOptions) *mcp.Server {
	version := a.version
	if version == "" {
		version = "dev"
	}
	server := mcp.NewServer(&mcp.Implementation{Name: a.name, Version: version},
		&mcp.ServerOptions{Instructions: a.description})
	for _, c := range a.commands {
		if !c.visibleIn(ProjectionMCP) || (c.risk == RiskDestructive && !opts.AllowDestructive) {
			continue
		}
		server.AddTool(a.mcpTool(c), a.mcpHandler(c))
	}
	return server
}

// mcpToolName maps a command path to a tool name: "tag add" -> "tag_add".
func mcpToolName(c *command) string { return strings.Join(c.path, "_") }

func (a *App) mcpTool(c *command) *mcp.Tool {
	input := c.input.inputSchema()
	if c.dryRunnable {
		input["properties"].(map[string]any)["dry-run"] = Schema{
			"type":        "boolean",
			"description": "Preview without making changes",
		}
	}
	destructive := c.risk == RiskDestructive
	tool := &mcp.Tool{
		Name:        mcpToolName(c),
		Description: c.description,
		InputSchema: input,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    c.risk == RiskRead,
			DestructiveHint: &destructive,
		},
	}
	// Older clients only accept object output schemas; other results are
	// still returned as JSON text.
	if out := outputSchema(c.outType); out["type"] == "object" {
		tool.OutputSchema = out
	}
	return tool
}

func (a *App) mcpHandler(c *command) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, dryRun, err := popDryRun(req.Params.Arguments, c.dryRunnable)
		if err != nil {
			return mcpError(err), nil
		}
		in, err := c.input.bindJSON(args)
		if err != nil {
			return mcpError(err), nil
		}
		out, err := c.run(&Context{Context: ctx, dryRun: dryRun, via: "mcp", stderr: a.stderr}, in)
		if err != nil {
			return mcpError(err), nil
		}
		data, err := toJSONValue(out)
		if err != nil {
			return mcpError(Errorf("ENCODE_FAILED", "cannot encode output: %v", err)), nil
		}
		text, _ := json.Marshal(data)
		res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}
		if _, ok := data.(map[string]any); ok {
			res.StructuredContent = data
		}
		return res, nil
	}
}

// popDryRun removes the "dry-run" argument, which is not part of the input type.
func popDryRun(args json.RawMessage, supported bool) (json.RawMessage, bool, error) {
	if !supported || len(args) == 0 {
		return args, false, nil
	}
	obj := map[string]json.RawMessage{}
	if err := json.Unmarshal(args, &obj); err != nil {
		return nil, false, Usage("INVALID_ARGUMENTS", "arguments must be a JSON object: %v", err)
	}
	raw, ok := obj["dry-run"]
	if !ok {
		return args, false, nil
	}
	var dryRun bool
	if err := json.Unmarshal(raw, &dryRun); err != nil {
		return nil, false, Usage("INVALID_VALUE", "dry-run must be a boolean")
	}
	delete(obj, "dry-run")
	rest, err := json.Marshal(obj)
	return rest, dryRun, err
}

func mcpError(err error) *mcp.CallToolResult {
	text, _ := json.Marshal(map[string]any{"error": asError(err)})
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}
}
