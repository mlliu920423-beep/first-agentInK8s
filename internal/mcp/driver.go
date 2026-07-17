package mcp

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/bigmay/first-agentink8s/internal/tools"
)

// Driver starts one MCP server of a specific transport type and registers
// its tools into reg. The returned io.Closer is invoked at shutdown.
//
// One Driver per transport ("inproc", "stdio", …). New transports = new
// driver package + init()-time RegisterDriver call. Loader is transport-
// agnostic.
type Driver interface {
	// Name is the transport identifier that the loader matches against
	// Config.Transport ("inproc" | "stdio").
	Name() string

	// Start brings up one MCP server per cfg and registers its tools with
	// reg. Returning a non-nil error is treated as a fatal boot failure
	// by the loader (fail-fast, see ADR-005) — do NOT downgrade internal
	// errors to nil/log.
	Start(ctx context.Context, cfg Config, reg *tools.Registry) (io.Closer, error)
}

var (
	driverMu       sync.RWMutex
	driverRegistry = map[string]Driver{}
)

// RegisterDriver is called from each transport package's init() so the
// loader can dispatch by cfg.Transport without importing the driver
// packages directly (avoids an import cycle: driver packages import this
// package for Config / Driver, loader would need to import them back).
//
// Blank-import the driver packages from cmd/server/main.go to activate
// registration.
func RegisterDriver(d Driver) {
	driverMu.Lock()
	defer driverMu.Unlock()
	name := d.Name()
	if _, ok := driverRegistry[name]; ok {
		panic(fmt.Sprintf("mcp: driver %q already registered", name))
	}
	driverRegistry[name] = d
}

// lookupDriver is used by loader.LoadAll; returns (nil, false) if no driver
// matches — the loader turns that into a hard error naming the transport.
func lookupDriver(transport string) (Driver, bool) {
	driverMu.RLock()
	defer driverMu.RUnlock()
	d, ok := driverRegistry[transport]
	return d, ok
}
