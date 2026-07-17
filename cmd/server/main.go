// Command server boots the Eino multi-agent demo.
//
// Bootstrap order:
//  1. Ark chat model  (fails fast if ARK_API_KEY / ARK_MODEL_ID missing)
//  2. Tool registry   (built-in Go tools)
//  3. Supervisor      (loads MCP servers + agent configs + specialists + host)
//  4. Global tool callback hook  (drives SSE tool_call/tool_result events)
//  5. HTTP server     (/api/chat SSE, /healthz, / static)
//
// On SIGINT/SIGTERM: shutdown HTTP server, then Supervisor.Shutdown
// closes every MCP client.
//
// The Supervisor abstraction (internal/agents/supervisor.go) owns the
// current *host.MultiAgent behind an atomic pointer and provides a
// transactional Rebuild(ctx) method. Phase 2 only exercises Rebuild
// from unit tests; Phase 3 will hang a REST endpoint on it. See
// docs/adr/006-registry-mutation-host-swap.md.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bigmay/first-agentink8s/internal/agents"
	"github.com/bigmay/first-agentink8s/internal/httpapi"
	"github.com/bigmay/first-agentink8s/internal/llm"
	"github.com/bigmay/first-agentink8s/internal/tools"
	"github.com/bigmay/first-agentink8s/internal/webassets"

	// Blank imports register the transport drivers with the mcp package.
	// Loader dispatches by cfg.Transport → whichever driver has claimed
	// that name in its init(); see docs/adr/005-mcp-driver-abstraction.md.
	_ "github.com/bigmay/first-agentink8s/internal/mcp/inproc"
	_ "github.com/bigmay/first-agentink8s/internal/mcp/stdio"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	port := envOr("PORT", "8080")
	agentsDir := envOr("AGENTS_DIR", "agents")
	mcpDir := envOr("MCP_DIR", "mcp")

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

	// 3. Supervisor — loads MCP + agents/*.yaml, builds specialists +
	// host, and hands us a *Supervisor whose Current() returns the
	// atomic *MultiAgent pointer. Fails fast on any startup error.
	sup, err := agents.NewSupervisor(ctx, agents.SupervisorDeps{
		Model:      arkModel,
		Registry:   reg,
		AgentsDir:  agentsDir,
		McpDir:     mcpDir,
		HostPrompt: agents.DefaultHostPrompt,
	})
	if err != nil {
		log.Printf("supervisor: %v", err)
		os.Exit(1)
	}
	log.Printf("tool registry: %v", reg.Names())

	// 4. Global tool callback hook — install ONCE at boot. Never call
	// this again (not even from Supervisor.Rebuild) — Eino's
	// callbacks.AppendGlobalHandlers is documented as init-once and
	// non-thread-safe. See docs/research/phase-2-host-swap-risks.md §3.
	httpapi.InstallToolCallbacks()

	// 5. HTTP server
	distFS, err := webassets.FS()
	if err != nil {
		log.Printf("webassets: %v", err)
		os.Exit(1)
	}
	apiSrv := &httpapi.Server{Sup: sup}

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
	if err := sup.Shutdown(shutdownCtx); err != nil {
		log.Printf("supervisor shutdown: %v", err)
	}
	log.Printf("bye")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
