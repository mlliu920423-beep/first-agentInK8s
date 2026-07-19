package mcp

// loader.go is the boring glue that ties Config (config.go), condition
// (cond.go), and Driver (driver.go) together. See config.go for the yaml
// schema and driver.go for the Driver interface — this file contains no
// transport-specific logic.

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/bigmay/first-agentink8s/internal/tools"
)

// LoadAll scans dir for *.yaml / *.yml MCP declarations, evaluates each
// file's enabled_if, and starts every enabled server via the appropriate
// driver. Returns all io.Closer handles so main.go can shut them down.
//
// Semantics (see ADR-005):
//   - enabled_if=false                  → skip, log one line, no error
//   - yaml parse error / unknown transport / typo enabled_if → hard error
//   - driver.Start returns err          → hard error; already-started
//     closers are Close()'d before returning so we leave no orphans
//   - empty dir / dir with only disabled entries → 0 closers, nil error
//     (unlike agentcfg, an MCP-less deployment is a legitimate future state)
//
// No goroutines: starts are sequential. Boot latency is dominated by the
// slowest external stdio init anyway, and sequential keeps failure modes
// simple (cleanup order is exactly reverse-start order).
func LoadAll(ctx context.Context, dir string, reg *tools.Registry) ([]io.Closer, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("mcp: dir %q does not exist, no MCP servers loaded", dir)
			return nil, nil
		}
		return nil, fmt.Errorf("read mcp dir %q: %w", dir, err)
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".yaml" || ext == ".yml" {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		log.Printf("mcp: no yaml files under %q, no MCP servers loaded", dir)
		return nil, nil
	}

	var closers []io.Closer
	seenName := map[string]string{}
	fail := func(err error) ([]io.Closer, error) {
		for _, c := range closers {
			_ = c.Close()
		}
		return nil, err
	}

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return fail(fmt.Errorf("read %s: %w", p, err))
		}
		cfg, err := parseConfig(p, raw)
		if err != nil {
			return fail(err)
		}
		if prev, ok := seenName[cfg.Name]; ok {
			return fail(fmt.Errorf("duplicate mcp name %q in %s (also in %s)", cfg.Name, p, prev))
		}
		seenName[cfg.Name] = p

		// enabled_if grammar was validated inside parseConfig; parse again
		// here purely to get the condition value. The re-parse cost is a
		// handful of nanoseconds and keeps the type off Config.
		cond, err := parseCondition(cfg.EnabledIf)
		if err != nil {
			return fail(fmt.Errorf("%s: enabled_if: %w", p, err))
		}
		if !cond.eval() {
			log.Printf("mcp: %s disabled (enabled_if=%q, source=%s)", cfg.Name, cond.raw, cfg.SourcePath())
			continue
		}

		drv, ok := lookupDriver(cfg.Transport)
		if !ok {
			return fail(fmt.Errorf("%s: no driver registered for transport %q", p, cfg.Transport))
		}
		c, err := drv.Start(ctx, cfg, reg)
		if err != nil {
			return fail(fmt.Errorf("%s: start %s: %w", p, cfg.Name, err))
		}
		if c != nil {
			closers = append(closers, c)
		}
		log.Printf("mcp: %s started (transport=%s, source=%s)", cfg.Name, cfg.Transport, cfg.SourcePath())
	}
	return closers, nil
}
