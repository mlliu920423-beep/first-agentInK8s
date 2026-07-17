package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/flow/agent/multiagent/host"

	"github.com/bigmay/first-agentink8s/internal/agentcfg"
	mcpbridge "github.com/bigmay/first-agentink8s/internal/mcp"
	"github.com/bigmay/first-agentink8s/internal/tools"
)

// Supervisor owns the current *host.MultiAgent and orchestrates atomic
// swaps of it on Rebuild.
//
// Concurrency model:
//
//   - Read side  (HandleChat): lock-free via atomic.Load
//   - Write side (Rebuild):    single-writer via rebuildMu
//
// A successful Rebuild is transactional: the new host is built into a
// scratch registry first, and only committed to the shared Registry +
// atomic pointer when every sub-step (agentcfg.Load, mcp.LoadAll,
// BuildSpecialist × N, BuildHost) succeeds. On any failure the old
// host keeps serving and the Registry is not mutated.
//
// See docs/specs/phase-2-registry-mutation-host-swap.md and
// docs/adr/006-registry-mutation-host-swap.md for the full design.
type Supervisor struct {
	current   atomic.Pointer[host.MultiAgent]
	rebuildMu sync.Mutex

	// Immutable dependencies set at NewSupervisor time.
	model      einomodel.ToolCallingChatModel
	reg        *tools.Registry
	agentsDir  string
	mcpDir     string
	hostPrompt string

	// mcpClosers holds the currently-live MCP driver closers. On a
	// successful Rebuild this slice is atomically replaced; the old
	// slice is Closed after gracePeriod so in-flight requests can
	// finish their MCP calls.
	closersMu  sync.Mutex
	mcpClosers []io.Closer

	// gracePeriod is how long after an atomic host swap we wait before
	// closing the old MCP drivers. Configurable via
	// SUPERVISOR_MCP_GRACE_PERIOD env var; default 30s.
	gracePeriod time.Duration
}

// SupervisorDeps groups the collaborators NewSupervisor needs. Field
// names line up with cmd/server/main.go local variables for readability.
type SupervisorDeps struct {
	Model      einomodel.ToolCallingChatModel
	Registry   *tools.Registry
	AgentsDir  string
	McpDir     string
	HostPrompt string
}

// defaultGracePeriod is the MVP value; matches the pre-refactor 30s
// init timeout for symmetry (see filesystem.go legacy behaviour).
const defaultGracePeriod = 30 * time.Second

// NewSupervisor performs the initial boot sequence: loads MCP servers,
// loads agent configs, builds specialists, builds the host multi-agent.
// It's a pure superset of the pre-Phase-2 main.go step 3~6.
//
// On any error, MCP drivers started so far are Closed and error is
// returned; the caller (main.go) should log and exit.
func NewSupervisor(ctx context.Context, deps SupervisorDeps) (*Supervisor, error) {
	if err := validateDeps(deps); err != nil {
		return nil, err
	}
	s := &Supervisor{
		model:       deps.Model,
		reg:         deps.Registry,
		agentsDir:   deps.AgentsDir,
		mcpDir:      deps.McpDir,
		hostPrompt:  deps.HostPrompt,
		gracePeriod: gracePeriodFromEnv(),
	}
	// Initial build: use the shared Registry directly (there's no old
	// state to preserve on first boot, so the transactional dry-run
	// dance from Rebuild is overkill here).
	mcpClosers, err := mcpbridge.LoadAll(ctx, s.mcpDir, s.reg)
	if err != nil {
		return nil, fmt.Errorf("mcp: %w", err)
	}
	s.mcpClosers = mcpClosers

	hostMA, err := s.buildHostFromRegistry(ctx, s.reg)
	if err != nil {
		for _, c := range mcpClosers {
			_ = c.Close()
		}
		return nil, err
	}
	s.current.Store(hostMA)
	return s, nil
}

// Current returns the current *host.MultiAgent. Callers should call it
// exactly once per request (typically as the first line of HandleChat)
// and use the returned pointer for the whole request lifetime —
// re-reading mid-request could split state across a concurrent Rebuild.
//
// Never returns nil after a successful NewSupervisor.
func (s *Supervisor) Current() *host.MultiAgent {
	return s.current.Load()
}

// Rebuild reloads agent configs + MCP servers and atomically swaps in
// a fresh *host.MultiAgent. Transactional: on any failure the old host
// keeps serving and the shared Registry is not mutated.
//
// Rebuild must NOT call callbacks.AppendGlobalHandlers /
// httpapi.InstallToolCallbacks() — those are init-once and non-thread-
// safe (see ADR-006 §Compliance).
//
// The passed ctx contributes values to downstream calls (agentcfg.Load,
// mcp.LoadAll, NewMultiAgent) but its cancellation is deliberately not
// propagated: rebuilding half-way and then bailing out on ctx.Done
// would leave a mess. We derive an internal ctx via context.WithoutCancel.
func (s *Supervisor) Rebuild(ctx context.Context) error {
	s.rebuildMu.Lock()
	defer s.rebuildMu.Unlock()

	log.Printf("supervisor: rebuild started (agentsDir=%s, mcpDir=%s)", s.agentsDir, s.mcpDir)

	// Derive a ctx whose values propagate but whose cancellation
	// doesn't — mid-rebuild cancel would leave scratch state hanging.
	ctx = context.WithoutCancel(ctx)

	// Step 1: dry-run into a scratch registry so failures don't touch
	// the live one.
	scratch := tools.NewRegistry()
	if err := tools.RegisterBuiltins(ctx, scratch); err != nil {
		return fmt.Errorf("rebuild: register builtins: %w", err)
	}

	// Step 2: start new MCP drivers into the scratch registry.
	newClosers, err := mcpbridge.LoadAll(ctx, s.mcpDir, scratch)
	if err != nil {
		// No commit happened yet. Close whatever drivers did start
		// before the error, and abort.
		for _, c := range newClosers {
			_ = c.Close()
		}
		return fmt.Errorf("rebuild: mcp: %w", err)
	}

	// Step 3: build new host multi-agent against scratch registry.
	newHost, err := s.buildHostFromRegistry(ctx, scratch)
	if err != nil {
		for _, c := range newClosers {
			_ = c.Close()
		}
		return fmt.Errorf("rebuild: %w", err)
	}

	// Step 4: All dry-run steps succeeded. Commit:
	//   4a. mergeRegistry: replace shared Registry contents with scratch
	//   4b. atomic.Store host pointer
	//   4c. swap closers slice; schedule delayed Close of the OLD ones
	if err := s.mergeRegistry(scratch); err != nil {
		// Extremely unlikely: only fails if Register returns error
		// (duplicate name in scratch, which is our own logic bug).
		// Roll back what we can and return.
		for _, c := range newClosers {
			_ = c.Close()
		}
		return fmt.Errorf("rebuild: merge registry: %w", err)
	}

	oldClosers := s.swapClosers(newClosers)
	s.current.Store(newHost)

	// Delayed Close of old MCP drivers so in-flight requests can finish.
	grace := s.gracePeriod
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("supervisor: panic in delayed Close goroutine: %v", r)
			}
		}()
		if grace > 0 {
			time.Sleep(grace)
		}
		for _, c := range oldClosers {
			if err := c.Close(); err != nil {
				log.Printf("supervisor: delayed close error: %v", err)
			}
		}
	}()

	log.Printf("supervisor: rebuild committed (%d MCP closers now live; %d old closers scheduled to close in %s)",
		len(newClosers), len(oldClosers), grace)
	return nil
}

// mergeRegistry copies scratch's contents into s.reg, replacing any
// existing entries. Names present in s.reg but not in scratch are
// Unregistered.
//
// The only concurrent reads of s.reg are MustResolve calls from
// in-flight requests, which already have their tool.BaseTool slices
// captured — they are unaffected by the underlying map changing here.
// Unregister is idempotent and Register only fails on duplicates, so
// we defensively Unregister before each Register on names that already
// exist in the live registry.
func (s *Supervisor) mergeRegistry(scratch *tools.Registry) error {
	oldNames := s.reg.Names()
	newNames := scratch.Names()

	newSet := make(map[string]struct{}, len(newNames))
	for _, n := range newNames {
		newSet[n] = struct{}{}
	}

	// Drop names that no longer exist in the new config.
	for _, n := range oldNames {
		if _, keep := newSet[n]; keep {
			continue
		}
		if err := s.reg.Unregister(n); err != nil {
			return fmt.Errorf("unregister %q: %w", n, err)
		}
	}

	// Copy every scratch entry into s.reg, replacing any pre-existing
	// entry (Register rejects duplicates, so Unregister first).
	for _, n := range newNames {
		t, ok := scratch.Get(n)
		if !ok {
			// Shouldn't happen: Names() derived from the same map.
			return fmt.Errorf("scratch registry lost entry %q between Names() and Get()", n)
		}
		if _, exists := s.reg.Get(n); exists {
			if err := s.reg.Unregister(n); err != nil {
				return fmt.Errorf("unregister %q before re-register: %w", n, err)
			}
		}
		if err := s.reg.Register(n, t); err != nil {
			return fmt.Errorf("register %q: %w", n, err)
		}
	}
	return nil
}

// swapClosers atomically replaces s.mcpClosers with newClosers and
// returns the previous slice.
func (s *Supervisor) swapClosers(newClosers []io.Closer) []io.Closer {
	s.closersMu.Lock()
	defer s.closersMu.Unlock()
	old := s.mcpClosers
	s.mcpClosers = newClosers
	return old
}

// Shutdown Closes every MCP driver currently owned by this Supervisor.
// Called from cmd/server/main.go on SIGINT/SIGTERM.
func (s *Supervisor) Shutdown(_ context.Context) error {
	s.closersMu.Lock()
	closers := s.mcpClosers
	s.mcpClosers = nil
	s.closersMu.Unlock()
	var firstErr error
	for _, c := range closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// buildHostFromRegistry runs the config-load → specialist-build →
// host-build chain against reg. Extracted so both NewSupervisor and
// Rebuild can call it (with reg = shared or scratch respectively).
func (s *Supervisor) buildHostFromRegistry(ctx context.Context, reg *tools.Registry) (*host.MultiAgent, error) {
	cfgs, err := agentcfg.Load(s.agentsDir)
	if err != nil {
		return nil, fmt.Errorf("agentcfg: %w", err)
	}
	specialists := make([]*host.Specialist, 0, len(cfgs))
	for _, cfg := range cfgs {
		sp, err := BuildSpecialist(ctx, s.model, cfg, reg)
		if err != nil {
			return nil, fmt.Errorf("build specialist %q: %w", cfg.Name, err)
		}
		specialists = append(specialists, sp)
	}
	hostMA, err := BuildHost(ctx, s.model, s.hostPrompt, specialists)
	if err != nil {
		return nil, fmt.Errorf("build host multi-agent: %w", err)
	}
	return hostMA, nil
}

func validateDeps(d SupervisorDeps) error {
	if d.Model == nil {
		return errors.New("SupervisorDeps.Model is required")
	}
	if d.Registry == nil {
		return errors.New("SupervisorDeps.Registry is required")
	}
	if d.AgentsDir == "" {
		return errors.New("SupervisorDeps.AgentsDir is required")
	}
	if d.McpDir == "" {
		return errors.New("SupervisorDeps.McpDir is required")
	}
	if d.HostPrompt == "" {
		return errors.New("SupervisorDeps.HostPrompt is required")
	}
	return nil
}

func gracePeriodFromEnv() time.Duration {
	v := os.Getenv("SUPERVISOR_MCP_GRACE_PERIOD")
	if v == "" {
		return defaultGracePeriod
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("supervisor: SUPERVISOR_MCP_GRACE_PERIOD=%q not a duration (%v); using default %s", v, err, defaultGracePeriod)
		return defaultGracePeriod
	}
	if d < 0 {
		log.Printf("supervisor: SUPERVISOR_MCP_GRACE_PERIOD=%q is negative; using default %s", v, defaultGracePeriod)
		return defaultGracePeriod
	}
	return d
}
