// Command server boots the Eino multi-agent demo.
//
// Bootstrap order:
//  1. Ark chat model  (fails fast if ARK_API_KEY / ARK_MODEL_ID missing)
//  2. Tool registry   (built-in Go tools)
//  3. Supervisor      (loads MCP servers + agent configs + specialists + host)
//  4. Global tool callback hook  (drives SSE tool_call/tool_result events)
//  5. Tracing         (Langfuse — no-op unless LANGFUSE_ENABLED=1)
//  6. Config store    (Phase 3 — backs the CRUD API)
//  7. HTTP server     (/api/chat SSE, /healthz, / static, /api/agents, /api/mcp)
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
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bigmay/first-agentink8s/internal/agents"
	"github.com/bigmay/first-agentink8s/internal/configstore"
	"github.com/bigmay/first-agentink8s/internal/httpapi"
	"github.com/bigmay/first-agentink8s/internal/llm"
	"github.com/bigmay/first-agentink8s/internal/tools"
	"github.com/bigmay/first-agentink8s/internal/tracing"
	"github.com/bigmay/first-agentink8s/internal/webassets"

	// Blank imports register the transport drivers with the mcp package.
	// Loader dispatches by cfg.Transport → whichever driver has claimed
	// that name in its init(); see docs/adr/005-mcp-driver-abstraction.md.
	_ "github.com/bigmay/first-agentink8s/internal/mcp/inproc"
	_ "github.com/bigmay/first-agentink8s/internal/mcp/stdio"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// --copy-configs mode: copies boot configs from read-only image paths
	// to writable directories. Used by the k8s init container so that the
	// Phase 3 CRUD API can write back to disk. Only works in k8s where
	// /data/agents and /data/mcp are emptyDir volumes.
	if len(os.Args) > 1 && os.Args[1] == "--copy-configs" {
		copyDir("/agents", "/data/agents")
		copyDir("/mcp", "/data/mcp")
		return
	}

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

	// 5. Tracing (Phase 5) — Langfuse observability. No-op unless
	// LANGFUSE_ENABLED=1 and auth env vars are set.
	tracingFlush, tracingEnabled, err := tracing.Setup()
	if err != nil {
		log.Printf("tracing: %v", err)
		os.Exit(1)
	}
	if tracingEnabled {
		defer tracingFlush()
		log.Printf("tracing: Langfuse flusher installed")
	}

	// 6. Config store (Phase 3) — backs the CRUD API
	store, err := configstore.NewStore(agentsDir, mcpDir)
	if err != nil {
		log.Printf("configstore: %v", err)
		os.Exit(1)
	}

	// 7. HTTP server — uses httprouter.NewRouter() for all routes:
	// /healthz, /api/chat, /api/agents/*, /api/mcp/*, /api/reload, /* SPA
	distFS, err := webassets.FS()
	if err != nil {
		log.Printf("webassets: %v", err)
		os.Exit(1)
	}
	apiSrv := &httpapi.Server{Sup: sup, Store: store}
	router := httpapi.NewRouter(apiSrv, distFS)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
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

// copyDir copies files from src to dst. Used by --copy-configs mode.
func copyDir(src, dst string) {
	if err := os.MkdirAll(dst, 0750); err != nil {
		log.Fatalf("copy-configs: mkdir %s: %v", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		log.Fatalf("copy-configs: read %s: %v", src, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		srcFile, err := os.Open(srcPath)
		if err != nil {
			log.Fatalf("copy-configs: open %s: %v", srcPath, err)
		}
		dstFile, err := os.Create(dstPath)
		if err != nil {
			srcFile.Close()
			log.Fatalf("copy-configs: create %s: %v", dstPath, err)
		}
		_, err = io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()
		if err != nil {
			log.Fatalf("copy-configs: copy %s: %v", srcPath, err)
		}
	}
	log.Printf("copy-configs: copied %d files from %s to %s", len(entries), src, dst)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
