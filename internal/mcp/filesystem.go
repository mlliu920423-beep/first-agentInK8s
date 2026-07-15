package mcp

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpproto "github.com/mark3labs/mcp-go/mcp"

	"github.com/bigmay/first-agentink8s/internal/tools"
)

// StartFilesystem wires the official `@modelcontextprotocol/server-filesystem`
// as an external MCP server over stdio, if enabled and if `npx` is on PATH.
//
// It's gated by ENABLE_FS_MCP=1 because it needs Node.js installed on the
// host (and it's therefore dev-only — the k8s distroless image doesn't have
// npx). Any failure — env not set, npx missing, subprocess crash — logs a
// warning and returns (nil, nil) so bootstrap continues.
//
// Root directory:
//
//	FS_MCP_ROOT env var, or the current working directory as a fallback.
//
// Registered tools appear under the `fs.*` namespace; the tool registry
// treats missing `fs.*` names as optional (see tools.MustResolve).
func StartFilesystem(ctx context.Context, reg *tools.Registry) (io.Closer, error) {
	if os.Getenv("ENABLE_FS_MCP") != "1" {
		return nil, nil
	}

	// Windows resolves npx.cmd via PATHEXT; LookPath handles that.
	npxPath, err := exec.LookPath("npx")
	if err != nil {
		log.Printf("mcp/filesystem: npx not on PATH, skipping external filesystem MCP")
		return nil, nil
	}

	root := os.Getenv("FS_MCP_ROOT")
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	root = firstExisting(root, ".")
	if root == "" {
		log.Printf("mcp/filesystem: no valid root, skipping")
		return nil, nil
	}

	log.Printf("mcp/filesystem: launching %q -y @modelcontextprotocol/server-filesystem %q", npxPath, root)
	cli, err := mcpclient.NewStdioMCPClient(
		npxPath, nil,
		"-y", "@modelcontextprotocol/server-filesystem", root,
	)
	if err != nil {
		log.Printf("mcp/filesystem: launch failed: %v (skipping)", err)
		return nil, nil
	}

	initReq := mcpproto.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpproto.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpproto.Implementation{Name: "eino-demo", Version: "0.1.0"}

	// `npx -y @modelcontextprotocol/server-filesystem` may download the package
	// on first run — cap the wait so a slow network doesn't block boot.
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := cli.Initialize(initCtx, initReq); err != nil {
		_ = cli.Close()
		log.Printf("mcp/filesystem: initialize failed: %v (skipping)", err)
		return nil, nil
	}

	list, err := einomcp.GetTools(ctx, &einomcp.Config{Cli: cli})
	if err != nil {
		_ = cli.Close()
		log.Printf("mcp/filesystem: GetTools failed: %v (skipping)", err)
		return nil, nil
	}
	for _, t := range list {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, err
		}
		name := "fs." + info.Name
		if err := reg.Register(name, t); err != nil {
			return nil, fmt.Errorf("register %s: %w", name, err)
		}
		log.Printf("mcp/filesystem: registered %s", name)
	}
	return cli, nil
}
