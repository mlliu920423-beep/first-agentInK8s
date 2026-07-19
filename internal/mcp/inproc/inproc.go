// Package inproc implements the "inproc" MCP transport driver.
//
// An inproc MCP server is a Go-native mcp-go server wired to an in-process
// client (no network, no subprocess). Each `provider` value selects a
// specific builtin implementation. Today the only provider is
// "builtin-demo", which exposes two toy tools (echo, list_dir) used by the
// workbuddy Phase 1 demo and by evals/routing.yaml.
//
// Adding a new inproc provider = adding a Go case in Start plus a
// startXxx() constructor in this package. New yaml file selects it via
// `provider: xxx`.
package inproc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpproto "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/bigmay/first-agentink8s/internal/mcp"
	"github.com/bigmay/first-agentink8s/internal/tools"
)

func init() {
	mcp.RegisterDriver(&driver{})
}

type driver struct{}

func (d *driver) Name() string { return "inproc" }

// Start dispatches on cfg.Provider to the matching builtin implementation.
// Any error is returned as-is so the loader can fail-fast per ADR-005 —
// this driver does NOT downgrade errors to warnings.
func (d *driver) Start(ctx context.Context, cfg mcp.Config, reg *tools.Registry) (io.Closer, error) {
	switch cfg.Provider {
	case "builtin-demo":
		return startBuiltinDemo(ctx, cfg, reg)
	default:
		return nil, fmt.Errorf("inproc: unknown provider %q (want: builtin-demo)", cfg.Provider)
	}
}

// startBuiltinDemo brings up the demo MCP server (echo + list_dir), wires
// an in-process client, initializes the protocol handshake, converts each
// server-side tool into an eino tool, and registers them under the "mcp.*"
// namespace. The returned io.Closer shuts the client down on process exit.
//
// Tool names / descriptions / parameter names are byte-for-byte identical
// to the pre-refactor implementation because agents/*.yaml and
// evals/routing.yaml both hard-reference them.
func startBuiltinDemo(ctx context.Context, cfg mcp.Config, reg *tools.Registry) (io.Closer, error) {
	srv := mcpserver.NewMCPServer(cfg.Name, "0.1.0")

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
			// Resolve path: caller "" or "." falls back to cfg.DefaultRoot,
			// then finally to ".". This fixes STATUS #1 where list_dir in
			// the distroless container returned [] because CWD is empty;
			// mcp/demo.yaml sets default_root: /agents.
			path := req.GetString("path", "")
			if path == "" || path == "." {
				if cfg.DefaultRoot != "" {
					path = cfg.DefaultRoot
				} else {
					path = "."
				}
			}
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
		return nil, fmt.Errorf("mcp[%s] inproc client: %w", cfg.Name, err)
	}
	if err := cli.Start(ctx); err != nil {
		return nil, fmt.Errorf("mcp[%s] inproc start: %w", cfg.Name, err)
	}
	initReq := mcpproto.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpproto.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpproto.Implementation{Name: "eino-demo", Version: "0.1.0"}
	if _, err := cli.Initialize(ctx, initReq); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("mcp[%s] inproc initialize: %w", cfg.Name, err)
	}

	list, err := einomcp.GetTools(ctx, &einomcp.Config{Cli: cli})
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("mcp[%s] GetTools: %w", cfg.Name, err)
	}
	for _, t := range list {
		info, err := t.Info(ctx)
		if err != nil {
			_ = cli.Close()
			return nil, fmt.Errorf("mcp[%s] tool info: %w", cfg.Name, err)
		}
		name := "mcp." + info.Name
		if err := reg.Register(name, t); err != nil {
			_ = cli.Close()
			return nil, fmt.Errorf("mcp[%s] register %s: %w", cfg.Name, name, err)
		}
		log.Printf("mcp[%s]: registered %s (%s)", cfg.Name, name, info.Desc)
	}
	return cli, nil
}
