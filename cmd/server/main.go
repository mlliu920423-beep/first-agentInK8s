// Command server boots the Eino multi-agent demo.
//
// Bootstrap order:
//  1. Ark chat model  (fails fast if ARK_API_KEY / ARK_MODEL_ID missing)
//  2. Tool registry   (built-in Go tools)
//  3. MCP sources     (in-proc always; external filesystem MCP if enabled)
//  4. Agent configs   (agents/*.yaml)
//  5. Specialists     (each = react.Agent wrapped as host.Specialist)
//  6. Host multi-agent
//  7. Global tool callback hook  (drives SSE tool_call/tool_result events)
//  8. HTTP server     (/api/chat SSE, /healthz, / static)
//
// On SIGINT/SIGTERM: shutdown HTTP server, then close every MCP client.
package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bigmay/first-agentink8s/internal/agentcfg"
	"github.com/bigmay/first-agentink8s/internal/agents"
	"github.com/bigmay/first-agentink8s/internal/httpapi"
	"github.com/bigmay/first-agentink8s/internal/llm"
	mcpbridge "github.com/bigmay/first-agentink8s/internal/mcp"
	"github.com/bigmay/first-agentink8s/internal/tools"
	"github.com/bigmay/first-agentink8s/internal/webassets"

	"github.com/cloudwego/eino/flow/agent/multiagent/host"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	port := envOr("PORT", "8080")
	agentsDir := envOr("AGENTS_DIR", "agents")

	// 1. Ark model
	arkModel, err := llm.NewArkModel(ctx)
	if err != nil {
		log.Printf("llm: %v", err)
		os.Exit(1)
	}

	// 2. Tool registry + built-ins
	reg := tools.NewRegistry()
	if err := tools.RegisterBuiltins(ctx, reg); err != nil {
		log.Printf("register builtins: %v", err)
		os.Exit(1)
	}

	// 3. MCP sources — warn-and-continue if any fail
	var closers []io.Closer
	if c, err := mcpbridge.StartInProc(ctx, reg); err != nil {
		log.Printf("mcp inproc: %v (continuing)", err)
	} else if c != nil {
		closers = append(closers, c)
	}
	if c, err := mcpbridge.StartFilesystem(ctx, reg); err != nil {
		log.Printf("mcp filesystem: %v (continuing)", err)
	} else if c != nil {
		closers = append(closers, c)
	}
	log.Printf("tool registry: %v", reg.Names())

	// 4. Agent YAML configs
	cfgs, err := agentcfg.Load(agentsDir)
	if err != nil {
		log.Printf("agentcfg: %v", err)
		os.Exit(1)
	}
	log.Printf("loaded %d agent configs from %s", len(cfgs), agentsDir)

	// 5. Specialists
	specialists := make([]*host.Specialist, 0, len(cfgs))
	for _, cfg := range cfgs {
		sp, err := agents.BuildSpecialist(ctx, arkModel, cfg, reg)
		if err != nil {
			log.Printf("build specialist %q: %v", cfg.Name, err)
			os.Exit(1)
		}
		specialists = append(specialists, sp)
		log.Printf("specialist ready: %s — %d tools", cfg.Name, len(cfg.Tools))
	}

	// 6. Host multi-agent
	hostMA, err := agents.BuildHost(ctx, arkModel, agents.DefaultHostPrompt, specialists)
	if err != nil {
		log.Printf("build host multi-agent: %v", err)
		os.Exit(1)
	}

	// 7. Global tool callback hook — must be installed before the first request
	httpapi.InstallToolCallbacks()

	// 8. HTTP server
	distFS, err := webassets.FS()
	if err != nil {
		log.Printf("webassets: %v", err)
		os.Exit(1)
	}
	apiSrv := &httpapi.Server{HostMA: hostMA}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", httpapi.Healthz)
	mux.HandleFunc("/api/chat", apiSrv.HandleChat)
	mux.Handle("/", httpapi.StaticHandler(distFS))

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on :%s (dev: http://localhost:%s)", port, port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http: %v", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Printf("shutdown signal received")

	shutdownCtx, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	for _, c := range closers {
		_ = c.Close()
	}
	log.Printf("bye")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
