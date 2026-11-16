package axon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"sync"
)

// The built-in MCP server implements the tools subset of the Model Context
// Protocol over stdio without third-party dependencies, which keeps Axon
// binaries small. Versions are listed newest first. Clients that speak
// 2026-07-28 first probe server/discover; it is answered with "method not
// found", which the specification defines as the signal to fall back to the
// initialize handshake.
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

// MCPOptions configures the MCP projection.
type MCPOptions struct {
	// AllowDestructive exposes destructive commands as tools. They are hidden
	// by default; when exposed, the MCP client is responsible for asking the
	// user, guided by the tool's destructiveHint annotation.
	AllowDestructive bool
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type mcpServer struct {
	app    *App
	tools  []map[string]any
	byName map[string]*command
}

// ServeMCP serves the application's commands as MCP tools, reading
// newline-delimited JSON-RPC from r and writing responses to w. It returns
// when r is exhausted or ctx is done.
func (a *App) ServeMCP(ctx context.Context, r io.Reader, w io.Writer, opts MCPOptions) error {
	s := &mcpServer{app: a, byName: map[string]*command{}}
	for _, c := range a.commands {
		if !c.visibleIn(ProjectionMCP) || (c.risk == RiskDestructive && !opts.AllowDestructive) {
			continue
		}
		s.tools = append(s.tools, a.mcpTool(c))
		s.byName[mcpToolName(c)] = c
	}

	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		br := bufio.NewReader(r)
		for {
			line, err := br.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				select {
				case lines <- line:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()

	var mu sync.Mutex
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErr:
			if err == io.EOF {
				return nil
			}
			return err
		case line := <-lines:
			if resp := s.handle(ctx, line); resp != nil {
				mu.Lock()
				err := enc.Encode(resp)
				mu.Unlock()
				if err != nil {
					return err
				}
			}
		}
	}
}

// handle processes one message and returns the response, or nil for
// notifications.
func (s *mcpServer) handle(ctx context.Context, line []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{rpcParseError, "parse error"}}
	}
	if len(req.ID) == 0 {
		return nil // notifications need no response
	}
	resp := &rpcResponse{JSONRPC: "2.0", ID: req.ID}
	result, rerr := s.dispatch(ctx, req)
	if rerr != nil {
		resp.Error = rerr
	} else {
		resp.Result = result
	}
	return resp
}

func (s *mcpServer) dispatch(ctx context.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := mcpVersions[0]
		if slices.Contains(mcpVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		v := s.app.version
		if v == "" {
			v = "dev"
		}
		result := map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.app.name, "version": v},
		}
		if s.app.description != "" {
			result["instructions"] = s.app.description
		}
		return result, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools := s.tools
		if tools == nil {
			tools = []map[string]any{}
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcError{rpcInvalidParams, "invalid params"}
		}
		c, ok := s.byName[p.Name]
		if !ok {
			return nil, &rpcError{rpcInvalidParams, "unknown tool: " + p.Name}
		}
		return s.app.callTool(ctx, c, p.Arguments), nil
	case "":
		return nil, &rpcError{rpcInvalidRequest, "invalid request"}
	}
	return nil, &rpcError{rpcMethodNotFound, "method not found: " + req.Method}
}

// mcpToolName maps a command path to a tool name: "tag add" -> "tag_add".
func mcpToolName(c *command) string { return strings.Join(c.path, "_") }

func (a *App) mcpTool(c *command) map[string]any {
	input := c.input.inputSchema()
	if c.dryRunnable {
		input["properties"].(map[string]any)["dry-run"] = Schema{
			"type":        "boolean",
			"description": "Preview without making changes",
		}
	}
	tool := map[string]any{
		"name":        mcpToolName(c),
		"description": c.description,
		"inputSchema": input,
		"annotations": map[string]any{
			"readOnlyHint":    c.risk == RiskRead,
			"destructiveHint": c.risk == RiskDestructive,
		},
	}
	// Clients before MCP 2026-07-28 only accept object output schemas; other
	// results are still returned as JSON text.
	if out := outputSchema(c.outType); out["type"] == "object" {
		tool["outputSchema"] = out
	}
	return tool
}

func (a *App) callTool(ctx context.Context, c *command, args json.RawMessage) map[string]any {
	args, dryRun, err := popDryRun(args, c.dryRunnable)
	if err != nil {
		return mcpError(err)
	}
	in, err := c.input.bindJSON(args)
	if err != nil {
		return mcpError(err)
	}
	out, err := c.run(&Context{Context: ctx, dryRun: dryRun, via: "mcp", stderr: a.stderr}, in)
	if err != nil {
		return mcpError(err)
	}
	data, err := toJSONValue(out)
	if err != nil {
		return mcpError(Errorf("ENCODE_FAILED", "cannot encode output: %v", err))
	}
	text, _ := json.Marshal(data)
	res := map[string]any{"content": []any{textContent(string(text))}}
	if _, ok := data.(map[string]any); ok {
		res["structuredContent"] = data
	}
	return res
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

func textContent(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

func mcpError(err error) map[string]any {
	text, _ := json.Marshal(map[string]any{"error": asError(err)})
	return map[string]any{"isError": true, "content": []any{textContent(string(text))}}
}
