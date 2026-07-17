package tools_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"

	"github.com/bigmay/first-agentink8s/internal/tools"
)

// The Registry stores tool.BaseTool values by name. To avoid importing
// eino's schema package just to build a stub, we borrow real built-in
// tools (calculator / weather / current_time) via RegisterBuiltins and
// re-register them under whatever names a test needs. Register only
// checks name uniqueness, not identity, so reusing the same underlying
// tool under different names is fine.

func newRegistryWithBuiltins(t *testing.T) *tools.Registry {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reg := tools.NewRegistry()
	if err := tools.RegisterBuiltins(ctx, reg); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	return reg
}

// borrowBuiltin returns a real tool.BaseTool (calculator) that we can
// re-Register under arbitrary test names for map-only assertions.
func borrowBuiltin(t *testing.T) tool.BaseTool {
	t.Helper()
	reg := newRegistryWithBuiltins(t)
	tl, ok := reg.Get("calculator")
	if !ok {
		t.Fatalf("borrowBuiltin: calculator missing after RegisterBuiltins")
	}
	return tl
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	reg := tools.NewRegistry()
	tl := borrowBuiltin(t)
	if err := reg.Register("foo", tl); err != nil {
		t.Fatalf("first Register: unexpected err %v", err)
	}
	err := reg.Register("foo", tl)
	if err == nil {
		t.Fatalf("second Register(foo): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error message %q missing 'already registered'", err.Error())
	}
}

func TestRegistry_Unregister_Existing(t *testing.T) {
	reg := newRegistryWithBuiltins(t)
	if err := reg.Unregister("calculator"); err != nil {
		t.Fatalf("Unregister(calculator): unexpected err %v", err)
	}
	if _, ok := reg.Get("calculator"); ok {
		t.Errorf("Get(calculator) after Unregister returned ok=true")
	}
	for _, n := range reg.Names() {
		if n == "calculator" {
			t.Errorf("Names() still contains calculator: %v", reg.Names())
		}
	}
}

func TestRegistry_Unregister_Idempotent(t *testing.T) {
	// (a) unregister on empty registry
	empty := tools.NewRegistry()
	if err := empty.Unregister("nope"); err != nil {
		t.Errorf("empty.Unregister: expected nil, got %v", err)
	}
	// (b) unregister a name that was never registered
	reg := newRegistryWithBuiltins(t)
	if err := reg.Unregister("never_registered"); err != nil {
		t.Errorf("Unregister(never_registered): expected nil, got %v", err)
	}
	// (c) unregister a registered name twice
	if err := reg.Unregister("calculator"); err != nil {
		t.Errorf("first Unregister(calculator): %v", err)
	}
	if err := reg.Unregister("calculator"); err != nil {
		t.Errorf("second Unregister(calculator): expected nil, got %v", err)
	}
}

// TestRegistry_Unregister_SliceReferencesStillWork encodes the Q1 SAFE
// argument from docs/research/phase-2-registry-mutation-design.md:
// a []tool.BaseTool captured before Unregister keeps working afterwards
// because the slice holds independent references, not the map entry.
func TestRegistry_Unregister_SliceReferencesStillWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg := newRegistryWithBuiltins(t)

	resolved, err := reg.MustResolve([]string{"calculator", "weather"})
	if err != nil {
		t.Fatalf("MustResolve: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("MustResolve len = %d, want 2", len(resolved))
	}

	// Rip both out of the registry.
	if err := reg.Unregister("calculator"); err != nil {
		t.Fatalf("Unregister(calculator): %v", err)
	}
	if err := reg.Unregister("weather"); err != nil {
		t.Fatalf("Unregister(weather): %v", err)
	}
	if _, ok := reg.Get("calculator"); ok {
		t.Fatalf("calculator still present in map after Unregister")
	}
	if _, ok := reg.Get("weather"); ok {
		t.Fatalf("weather still present in map after Unregister")
	}

	// Info() on both slice entries must still succeed — the slice
	// reference is independent of the map entry.
	calcInfo, err := resolved[0].Info(ctx)
	if err != nil {
		t.Fatalf("resolved[0].Info: %v", err)
	}
	if calcInfo == nil || calcInfo.Name != "calculator" {
		t.Errorf("resolved[0].Info name = %+v, want calculator", calcInfo)
	}
	weatherInfo, err := resolved[1].Info(ctx)
	if err != nil {
		t.Fatalf("resolved[1].Info: %v", err)
	}
	if weatherInfo == nil || weatherInfo.Name != "weather" {
		t.Errorf("resolved[1].Info name = %+v, want weather", weatherInfo)
	}

	// Stronger claim: actually invoke calculator via InvokableRun with a
	// canned JSON argument. This proves the tool is not just readable but
	// still executable after Unregister.
	inv, ok := resolved[0].(tool.InvokableTool)
	if !ok {
		t.Fatalf("resolved[0] does not implement InvokableTool")
	}
	out, err := inv.InvokableRun(ctx, `{"a":2,"op":"+","b":3}`)
	if err != nil {
		t.Fatalf("InvokableRun on unregistered calculator: %v", err)
	}
	if !strings.Contains(out, "5") {
		t.Errorf("InvokableRun output = %q, want a result containing 5", out)
	}
}

func TestRegistry_ConcurrentReadWrite(t *testing.T) {
	// Run with `go test -race` — the point is to prove there are no data
	// races on the underlying map.
	reg := tools.NewRegistry()
	tl := borrowBuiltin(t)

	// Seed with one entry so readers have something to find.
	if err := reg.Register("seed", tl); err != nil {
		t.Fatalf("seed Register: %v", err)
	}

	const iters = 300
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: churn names in and out.
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			name := fmt.Sprintf("tool-%d", i)
			_ = reg.Register(name, tl)
			_ = reg.Unregister(name)
		}
	}()

	// Reader: Get / Names / MustResolve concurrently.
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_, _ = reg.Get("seed")
			_ = reg.Names()
			_, _ = reg.MustResolve([]string{"seed"})
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("concurrent test timed out — possible deadlock")
	}
}

func TestRegistry_MustResolve_FsOptional(t *testing.T) {
	reg := newRegistryWithBuiltins(t)
	// fs.read_file is deliberately not registered — Phase 2 behavior is
	// to skip fs.* silently (log line) and still succeed.
	got, err := reg.MustResolve([]string{"calculator", "fs.read_file"})
	if err != nil {
		t.Fatalf("MustResolve: unexpected err %v", err)
	}
	if len(got) != 1 {
		t.Errorf("resolved len = %d, want 1 (calculator only)", len(got))
	}
	info, err := got[0].Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "calculator" {
		t.Errorf("resolved[0] name = %q, want calculator", info.Name)
	}
}

func TestRegistry_MustResolve_UnknownName_Fails(t *testing.T) {
	reg := newRegistryWithBuiltins(t)
	_, err := reg.MustResolve([]string{"calculator", "not_a_real_tool"})
	if err == nil {
		t.Fatalf("MustResolve: expected error for unknown name, got nil")
	}
	if !strings.Contains(err.Error(), "not_a_real_tool") {
		t.Errorf("error message %q missing 'not_a_real_tool'", err.Error())
	}
}

func TestRegistry_Names_Sorted(t *testing.T) {
	reg := tools.NewRegistry()
	tl := borrowBuiltin(t)
	// Insert in non-sorted order.
	for _, n := range []string{"z-tool", "a-tool", "m-tool"} {
		if err := reg.Register(n, tl); err != nil {
			t.Fatalf("Register(%q): %v", n, err)
		}
	}
	got := reg.Names()
	want := []string{"a-tool", "m-tool", "z-tool"}
	if len(got) != len(want) {
		t.Fatalf("Names len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}
