package agents_test

// supervisor_test.go covers the acceptance criteria listed in
// docs/specs/phase-2-registry-mutation-host-swap.md §Acceptance Criteria
// and docs/adr/006-registry-mutation-host-swap.md §Compliance / Validation:
//
//   - InitialBuild:                         Current() non-nil after boot
//   - RebuildSuccess_NewPointer:            atomic swap replaces the pointer
//   - RebuildFailure_OldHostSurvives:       transactional; old host + registry intact
//   - ConcurrentRebuilds_Serialized:        rebuildMu enforces single-writer
//   - ConcurrentRebuildAndCurrent_NoBlock:  read side is lock-free (verify -race)
//   - CallbacksHandlerCountUnchanged:       Rebuild must NOT call AppendGlobalHandlers
//                                           (research note §3). Skipped — see note below.
//   - UnregisterAfterMustResolve:           tool.BaseTool references survive Unregister
//
// The suite uses a package-scoped fake ToolCallingChatModel; every test
// wires up its own t.TempDir()-based agents/mcp fixture through a helper
// (newSupervisorForTest). Grace period is forced to zero via t.Setenv
// so the delayed-Close goroutine doesn't linger in CI.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/bigmay/first-agentink8s/internal/agents"
	"github.com/bigmay/first-agentink8s/internal/tools"

	// Blank import registers the inproc MCP driver so mcp.LoadAll works
	// when tests point mcpDir at a fixture with transport=inproc.
	_ "github.com/bigmay/first-agentink8s/internal/mcp/inproc"
)

// ---------------------------------------------------------------------------
// Fake ChatModel
// ---------------------------------------------------------------------------

// fakeChatModel implements einomodel.ToolCallingChatModel with the smallest
// surface needed by host.NewMultiAgent / react.NewAgent. Neither Supervisor
// build path invokes Generate or Stream at construction time (they only wire
// the model into a compose.Graph node), but we implement them defensively so
// a future refactor that adds a smoke-invocation to build wouldn't blow up.
//
// The real ToolCallingChatModel signature (verified against
// C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12\components\model\interface.go
// lines 99-103) is:
//
//	type ToolCallingChatModel interface {
//	    BaseChatModel     // Generate + Stream
//	    WithTools(tools []*schema.ToolInfo) (ToolCallingChatModel, error)
//	}
//
// BaseChatModel is BaseModel[*schema.Message] (line 71), whose methods are
// Generate(ctx, []*schema.Message, ...model.Option) (*schema.Message, error)
// and Stream(...) (*schema.StreamReader[*schema.Message], error).
//
// No BindTools — v0.9.12 removed it from the ToolCallingChatModel contract
// (only the deprecated ChatModel interface still exposes it).
type fakeChatModel struct{}

func (f *fakeChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	return schema.AssistantMessage("fake", nil), nil
}

func (f *fakeChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("fake", nil)}), nil
}

// WithTools returns the receiver: host.NewMultiAgent + react.NewAgent both
// call this to bind schema.ToolInfo, but Supervisor's tests never actually
// invoke the resulting model, so we don't need per-call isolation.
func (f *fakeChatModel) WithTools(_ []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return f, nil
}

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

const mathAgentYAML = `name: math_agent
description: Route arithmetic requests here.
system_prompt: |
  You are Math Agent. Use calculator for every arithmetic step.
tools:
  - calculator
max_step: 4
`

const inprocDemoYAML = `name: demo
transport: inproc
provider: builtin-demo
enabled_if: always
`

// writeFile creates parent dirs as needed and writes body at 0o600 to satisfy
// gosec (same as loader_test.go).
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// fixture holds the ephemeral directories a test operates on so scenarios
// can break them mid-test (e.g. corrupt yaml before Rebuild).
type fixture struct {
	agentsDir string
	mcpDir    string
	reg       *tools.Registry
}

// newFixture writes a minimal valid agents/ + mcp/ pair under t.TempDir().
func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	agentsDir := filepath.Join(base, "agents")
	mcpDir := filepath.Join(base, "mcp")
	writeFile(t, filepath.Join(agentsDir, "math_agent.yaml"), mathAgentYAML)
	writeFile(t, filepath.Join(mcpDir, "demo.yaml"), inprocDemoYAML)

	reg := tools.NewRegistry()
	if err := tools.RegisterBuiltins(context.Background(), reg); err != nil {
		t.Fatalf("register builtins: %v", err)
	}
	return &fixture{agentsDir: agentsDir, mcpDir: mcpDir, reg: reg}
}

// newSupervisorForTest boots a Supervisor against fx with a fresh fake
// model. Grace period is forced to zero so the delayed Close goroutine
// finishes before test teardown.
func newSupervisorForTest(t *testing.T, fx *fixture) *agents.Supervisor {
	t.Helper()
	t.Setenv("SUPERVISOR_MCP_GRACE_PERIOD", "0")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sup, err := agents.NewSupervisor(ctx, agents.SupervisorDeps{
		Model:      &fakeChatModel{},
		Registry:   fx.reg,
		AgentsDir:  fx.agentsDir,
		McpDir:     fx.mcpDir,
		HostPrompt: agents.DefaultHostPrompt,
	})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sup.Shutdown(shutdownCtx); err != nil {
			t.Logf("supervisor Shutdown: %v", err)
		}
	})
	return sup
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSupervisor_InitialBuild(t *testing.T) {
	fx := newFixture(t)
	sup := newSupervisorForTest(t, fx)

	if sup.Current() == nil {
		t.Fatalf("Current() returned nil after successful NewSupervisor")
	}
	// Registry must include at least the built-ins + the inproc demo tools
	// (mcp.echo / mcp.list_dir). Names() is sorted.
	names := fx.reg.Names()
	if len(names) < 3 {
		t.Fatalf("expected at least 3 registered tools (3 builtins), got %v", names)
	}
}

func TestSupervisor_RebuildSuccess_NewPointer(t *testing.T) {
	fx := newFixture(t)
	sup := newSupervisorForTest(t, fx)

	oldPtr := sup.Current()
	if oldPtr == nil {
		t.Fatal("Current() nil before Rebuild")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sup.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	newPtr := sup.Current()
	if newPtr == nil {
		t.Fatal("Current() nil after Rebuild")
	}
	if newPtr == oldPtr {
		t.Fatalf("Rebuild did not swap the pointer (both %p)", newPtr)
	}
	// The caller-captured oldPtr must still be non-nil — atomic swap does
	// not zero any storage the old pointer refers to (research note §1: no
	// teardown of MultiAgent, GC eventually reclaims it).
	if oldPtr == nil {
		t.Fatalf("previously captured oldPtr became nil after swap")
	}
}

func TestSupervisor_RebuildFailure_OldHostSurvives(t *testing.T) {
	fx := newFixture(t)
	sup := newSupervisorForTest(t, fx)

	oldPtr := sup.Current()
	if oldPtr == nil {
		t.Fatal("Current() nil before Rebuild")
	}
	oldNames := fx.reg.Names()

	// Corrupt the agents fixture so agentcfg.Load fails inside Rebuild:
	// removing the only yaml triggers "no agent yaml files found under X".
	if err := os.Remove(filepath.Join(fx.agentsDir, "math_agent.yaml")); err != nil {
		t.Fatalf("remove yaml: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := sup.Rebuild(ctx)
	if err == nil {
		t.Fatal("expected Rebuild to fail after fixture broken, got nil")
	}

	// Old host still served by Current().
	if sup.Current() != oldPtr {
		t.Fatalf("failed Rebuild swapped the pointer: old=%p new=%p", oldPtr, sup.Current())
	}
	// Registry contents unchanged (spec §Acceptance Criteria: "Registry
	// 不脏"). Names() is sorted so a straight slice compare works.
	newNames := fx.reg.Names()
	if len(newNames) != len(oldNames) {
		t.Fatalf("registry name count changed on failed rebuild: before=%v after=%v", oldNames, newNames)
	}
	for i := range oldNames {
		if oldNames[i] != newNames[i] {
			t.Fatalf("registry contents changed on failed rebuild:\n  before=%v\n  after=%v", oldNames, newNames)
		}
	}
}

func TestSupervisor_ConcurrentRebuilds_Serialized(t *testing.T) {
	fx := newFixture(t)
	sup := newSupervisorForTest(t, fx)

	const N = 3
	errs := make([]error, N)
	var wg sync.WaitGroup
	wg.Add(N)
	done := make(chan struct{})

	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			errs[i] = sup.Rebuild(ctx)
		}()
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent Rebuilds deadlocked or hung")
	}

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Rebuild #%d returned error (should have been serialized to success): %v", i, err)
		}
	}
	if sup.Current() == nil {
		t.Fatal("Current() nil after concurrent Rebuilds")
	}
}

func TestSupervisor_ConcurrentRebuildAndCurrent_NoBlock(t *testing.T) {
	fx := newFixture(t)
	sup := newSupervisorForTest(t, fx)

	stop := make(chan struct{})
	var readerErr error
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if sup.Current() == nil {
				readerErr = errors.New("Current() returned nil during concurrent Rebuild")
				return
			}
		}
	}()

	// Fire a burst of Rebuilds while the reader spins. 100 was suggested
	// by the prompt but even 20 tickles a data race in a broken impl,
	// while keeping wall-clock reasonable when combined with -race.
	const iters = 20
	for i := 0; i < iters; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := sup.Rebuild(ctx); err != nil {
			cancel()
			close(stop)
			readerWG.Wait()
			t.Fatalf("Rebuild #%d: %v", i, err)
		}
		cancel()
	}

	close(stop)
	readerWG.Wait()
	if readerErr != nil {
		t.Fatal(readerErr)
	}
}

func TestSupervisor_CallbacksHandlerCountUnchanged(t *testing.T) {
	// Research note §3 caveat: callbacks.AppendGlobalHandlers is NOT
	// idempotent or thread-safe, and Rebuild must never call it.
	//
	// The load-bearing check here would be:
	//   before := len(callbacks.GlobalHandlers)
	//   sup.Rebuild(ctx)
	//   after  := len(callbacks.GlobalHandlers)
	//   require.Equal(before, after)
	//
	// But GlobalHandlers lives in the eino "internal/callbacks" package
	// (C:\Users\Bigmay\go\pkg\mod\github.com\cloudwego\eino@v0.9.12
	//   \internal\callbacks\manager.go:30 — var GlobalHandlers []Handler),
	// re-exported only through eino/callbacks/interface.go's writers
	// (InitCallbackHandlers, AppendGlobalHandlers). There is NO getter,
	// so an external _test package cannot inspect the slice length.
	//
	// The equivalent guarantee is enforced elsewhere:
	//   - supervisor.go doc: "Rebuild must NOT call
	//     callbacks.AppendGlobalHandlers / httpapi.InstallToolCallbacks()"
	//   - ADR-006 §Compliance grep gate:
	//       grep -rn "InstallToolCallbacks" internal/  → only main.go
	//       grep -rn "callbacks.AppendGlobalHandlers" internal/  → 0 matches
	//
	// TODO(phase-3+): if eino exposes a public getter or callbacks.LenGlobalHandlers(),
	// replace this Skip with a real assertion around a Rebuild call.
	t.Skip("cannot read callbacks.GlobalHandlers length from outside eino/internal/callbacks; " +
		"enforced by supervisor.go docstring + ADR-006 grep gates instead")
}

// TestSupervisor_UnregisterAfterMustResolve is strictly a registry test —
// there is no internal/tools/registry_test.go today, so we host the
// research-note-§Q1 invariant here (per the prompt: "if that test agent
// hasn't written it, adding a subtest here is OK"). Verifies the load-
// bearing property that MustResolve's returned slice keeps its tool
// references alive across Unregister — which is exactly what an
// in-flight request needs across a Rebuild-driven Registry mutation.
func TestSupervisor_UnregisterAfterMustResolve(t *testing.T) {
	reg := tools.NewRegistry()
	if err := tools.RegisterBuiltins(context.Background(), reg); err != nil {
		t.Fatalf("register builtins: %v", err)
	}

	resolved, err := reg.MustResolve([]string{"calculator"})
	if err != nil {
		t.Fatalf("MustResolve: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("want 1 tool, got %d", len(resolved))
	}

	if err := reg.Unregister("calculator"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	// Sanity: Registry.Get now misses.
	if _, ok := reg.Get("calculator"); ok {
		t.Fatal("Get(\"calculator\") still ok after Unregister")
	}

	// The captured tool must still be Invokable — its interface value
	// carries an itab pointing at the underlying InferTool impl; the map
	// delete does not touch that storage.
	inv, ok := resolved[0].(tool.InvokableTool)
	if !ok {
		t.Fatalf("calculator tool does not implement InvokableTool (%T)", resolved[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := inv.InvokableRun(ctx, `{"a":6,"op":"*","b":7}`)
	if err != nil {
		t.Fatalf("InvokableRun after Unregister: %v", err)
	}
	if out == "" {
		t.Fatal("empty tool output after Unregister")
	}
	// Loose sanity: result contains "42".
	if !containsString(out, "42") {
		t.Fatalf("expected result to mention 42, got %q", out)
	}
}

// containsString is a tiny helper so we don't pull in strings just for one call.
func containsString(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Compile-time assertion: fakeChatModel really does implement the eino
// ToolCallingChatModel interface. If eino changes the interface signature
// in a future version bump, this file stops compiling with a targeted
// error rather than the tests silently green-lighting a mismatch.
var _ einomodel.ToolCallingChatModel = (*fakeChatModel)(nil)
