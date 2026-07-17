// Package stdio implements the generic stdio-transport MCP driver.
//
// One process per MCP server, launched as a child of this program, speaking
// the MCP JSON-RPC framing over its stdin/stdout. Configuration is entirely
// declarative — see any mcp/*.yaml with `transport: stdio`.
//
// The official `@modelcontextprotocol/server-filesystem` is one instance of
// this driver (via mcp/filesystem.yaml); nothing here is filesystem-specific.
// New stdio MCP servers = new yaml file, no Go code change.
//
// Tool namespace: every tool exposed by the child process is registered as
// `<cfg.Name>.<tool.Info.Name>`. cfg.Name doubles as the log prefix (so the
// filesystem yaml uses `name: fs` to preserve the historical `fs.*` tool
// prefix while still identifying the server in logs).
//
// Failure policy: fail-fast. Any error during LookPath / spawn / Initialize /
// GetTools / Register propagates up as a fatal boot error so the pod
// crashloops with a clear message, instead of silently missing tools that
// downstream agents will then fail to resolve at runtime (ADR-005 §Decision).
package stdio

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpproto "github.com/mark3labs/mcp-go/mcp"

	"github.com/bigmay/first-agentink8s/internal/mcp"
	"github.com/bigmay/first-agentink8s/internal/tools"
)

func init() {
	mcp.RegisterDriver(&driver{})
}

type driver struct{}

func (d *driver) Name() string { return "stdio" }

// Start launches one stdio MCP server per cfg and registers its tools.
//
// On any error the client (if already spawned) is closed before return so
// no child process is leaked. On success the returned io.Closer is the live
// client; the loader closes it at shutdown.
func (d *driver) Start(ctx context.Context, cfg mcp.Config, reg *tools.Registry) (io.Closer, error) {
	logPrefix := "mcp/" + cfg.Name

	// Windows resolves .cmd / .exe via PATHEXT; LookPath handles that.
	fullCmd, err := exec.LookPath(cfg.Command)
	if err != nil {
		return nil, fmt.Errorf("%s: command %q not on PATH: %w", logPrefix, cfg.Command, err)
	}

	env := os.Environ()
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}

	log.Printf("%s: launching %q %v", logPrefix, fullCmd, cfg.Args)
	cli, err := mcpclient.NewStdioMCPClient(fullCmd, env, cfg.Args...)
	if err != nil {
		return nil, fmt.Errorf("%s: launch failed: %w", logPrefix, err)
	}

	initReq := mcpproto.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpproto.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpproto.Implementation{Name: "eino-demo", Version: "0.1.0"}

	// npx-launched servers may download the package on first run; cap the wait
	// so a slow network doesn't hang boot. Duration comes from the yaml (or
	// its default) so operators can tune per-server.
	initCtx, cancel := context.WithTimeout(ctx, cfg.InitTimeoutOrDefault())
	defer cancel()
	if _, err := cli.Initialize(initCtx, initReq); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("%s: initialize failed: %w", logPrefix, err)
	}

	list, err := einomcp.GetTools(ctx, &einomcp.Config{Cli: cli})
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("%s: GetTools failed: %w", logPrefix, err)
	}
	for _, t := range list {
		info, err := t.Info(ctx)
		if err != nil {
			_ = cli.Close()
			return nil, fmt.Errorf("%s: tool.Info failed: %w", logPrefix, err)
		}
		name := cfg.Name + "." + info.Name
		if err := reg.Register(name, t); err != nil {
			_ = cli.Close()
			return nil, fmt.Errorf("%s: register %s: %w", logPrefix, name, err)
		}
		log.Printf("%s: registered %s", logPrefix, name)
	}
	return cli, nil
}
