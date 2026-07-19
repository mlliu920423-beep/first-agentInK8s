// Package tools provides a name → tool.BaseTool registry.
//
// Every capability the agents can invoke — built-in Go functions or tools
// bridged in from an MCP server — is registered here under a stable string
// name. Sub-agent YAML files reference these names in their `tools:` field;
// the agent builder resolves them at startup.
package tools

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/tool"
)

// Registry is a concurrent-safe map from tool name to tool.BaseTool.
type Registry struct {
	mu sync.RWMutex
	m  map[string]tool.BaseTool
}

func NewRegistry() *Registry {
	return &Registry{m: make(map[string]tool.BaseTool)}
}

// Register adds a tool under name. Duplicate names are rejected.
func (r *Registry) Register(name string, t tool.BaseTool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[name]; ok {
		return fmt.Errorf("tool %q already registered", name)
	}
	r.m[name] = t
	return nil
}

// Unregister removes name from the registry.
//
// Idempotent: if name is not registered, returns nil (with a debug log
// line). Rationale — Supervisor.Rebuild flows may call Unregister on
// names that never got registered (e.g. an fs.* tool when filesystem
// MCP is disabled). Making this a hard error would force every caller
// to wrap it in `if _, ok := r.Get(name); ok { r.Unregister(name) }`.
//
// IMPORTANT: Unregister only removes the map entry. Any []tool.BaseTool
// slice previously returned by MustResolve keeps its references and
// remains invokable — the tool's underlying resources (e.g. MCP client)
// are owned by whoever holds the driver's io.Closer, not by the
// Registry. See docs/adr/006-registry-mutation-host-swap.md.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[name]; !ok {
		log.Printf("tools: Unregister(%q) — not registered, no-op", name)
		return nil
	}
	delete(r.m, name)
	return nil
}

// Get returns the tool and whether it exists.
func (r *Registry) Get(name string) (tool.BaseTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.m[name]
	return t, ok
}

// Names returns registered names sorted alphabetically (stable for logging).
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.m))
	for k := range r.m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MustResolve looks up the given names.
//
// Names with the `fs.` prefix are treated as OPTIONAL — if the external
// filesystem MCP server isn't enabled they'll be missing, but the agent
// should still start. A warning is logged and the missing name is skipped.
//
// Any other missing name is a hard error, since it indicates a typo in a
// YAML config or a bug in the builtin registration.
func (r *Registry) MustResolve(names []string) ([]tool.BaseTool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]tool.BaseTool, 0, len(names))
	var missing []string
	for _, n := range names {
		t, ok := r.m[n]
		if !ok {
			if strings.HasPrefix(n, "fs.") {
				log.Printf("tools: optional tool %q not registered (external MCP disabled?), skipping", n)
				continue
			}
			missing = append(missing, n)
			continue
		}
		out = append(out, t)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("unknown tool(s): %s (registered: %v)", strings.Join(missing, ", "), r.namesLocked())
	}
	return out, nil
}

func (r *Registry) namesLocked() []string {
	out := make([]string, 0, len(r.m))
	for k := range r.m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// RegisterBuiltins wires the three demo skills into the registry.
func RegisterBuiltins(_ context.Context, r *Registry) error {
	for _, entry := range []struct {
		name string
		make func() (tool.BaseTool, error)
	}{
		{"calculator", newCalculatorTool},
		{"weather", newWeatherTool},
		{"current_time", newCurrentTimeTool},
	} {
		t, err := entry.make()
		if err != nil {
			return fmt.Errorf("register builtin %q: %w", entry.name, err)
		}
		if err := r.Register(entry.name, t); err != nil {
			return err
		}
	}
	return nil
}
