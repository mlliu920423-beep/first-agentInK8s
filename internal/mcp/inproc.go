// Package mcp bridges MCP servers into the tool.Registry so agents can call
// them just like built-in Go tools.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpproto "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/bigmay/first-agentink8s/internal/tools"
)

// StartInProc constructs an in-process demo MCP server exposing two toy
// tools (`echo`, `list_dir`), wires an in-process client to it, and
// registers the resulting eino tools under the `mcp.*` namespace.
//
// Returns an io.Closer that shuts down the client when called; main.go
// defers it on shutdown. Errors from this bootstrap are treated as
// warn-and-continue by callers so a broken MCP path doesn't break the demo.
func StartInProc(ctx context.Context, reg *tools.Registry) (io.Closer, error) {
	srv := mcpserver.NewMCPServer("demo-inproc", "0.1.0")

	srv.AddTool(
		mcpproto.NewTool("echo",
			mcpproto.WithDescription("Return the text argument unchanged. Useful for testing MCP wiring."),
			mcpproto.WithString("text", mcpproto.Description("text to echo back")),
		),
		func(ctx context.Context, req mcpproto.CallToolRequest) (*mcpproto.CallToolResult, error) {
			text, err := req.RequireString("text")
			if err != nil {
				return mcpproto.NewToolResultError(err.Error()), nil
			}
			return mcpproto.NewToolResultText(text), nil
		},
	)

	srv.AddTool(
		mcpproto.NewTool("list_dir",
			mcpproto.WithDescription("List entries of a directory on the server host. Returns a JSON array of {name, is_dir}. Path defaults to the working directory."),
			mcpproto.WithString("path", mcpproto.Description("absolute or relative path; defaults to '.'")),
		),
		func(ctx context.Context, req mcpproto.CallToolRequest) (*mcpproto.CallToolResult, error) {
			path := req.GetString("path", ".")
			entries, err := os.ReadDir(path)
			if err != nil {
				return mcpproto.NewToolResultErrorFromErr("read dir", err), nil
			}
			out := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				out = append(out, map[string]any{"name": e.Name(), "is_dir": e.IsDir()})
			}
			b, _ := json.Marshal(out)
			return mcpproto.NewToolResultText(string(b)), nil
		},
	)

	cli, err := mcpclient.NewInProcessClient(srv)
	if err != nil {
		return nil, fmt.Errorf("mcp inproc client: %w", err)
	}
	if err := cli.Start(ctx); err != nil {
		return nil, fmt.Errorf("mcp inproc start: %w", err)
	}
	initReq := mcpproto.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpproto.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpproto.Implementation{Name: "eino-demo", Version: "0.1.0"}
	if _, err := cli.Initialize(ctx, initReq); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("mcp inproc initialize: %w", err)
	}

	list, err := einomcp.GetTools(ctx, &einomcp.Config{Cli: cli})
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("mcp GetTools: %w", err)
	}
	for _, t := range list {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, err
		}
		name := "mcp." + info.Name
		if err := reg.Register(name, t); err != nil {
			return nil, err
		}
		log.Printf("mcp: registered %s (%s)", name, info.Desc)
	}
	return cli, nil
}

// firstExisting is a tiny helper for filesystem-mcp root selection.
func firstExisting(cands ...string) string {
	for _, c := range cands {
		if c == "" {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
			return abs
		}
	}
	return ""
}
