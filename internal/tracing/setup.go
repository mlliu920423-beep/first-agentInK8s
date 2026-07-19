// Package tracing wires up the Langfuse callback handler for Eino.
//
// Usage:
//
//	flusher, enabled, err := tracing.Setup(ctx)
//	if err != nil { log.Fatal(err) }
//	if enabled {
//	    defer flusher()
//	}
package tracing

import (
	"fmt"
	"log"
	"os"

	"github.com/cloudwego/eino-ext/callbacks/langfuse"
	"github.com/cloudwego/eino/callbacks"
)

// Setup initializes the Langfuse callback handler and registers it globally.
// Returns a flusher that must be called on server shutdown to flush pending
// trace events to Langfuse.
//
// If LANGFUSE_ENABLED is not "1", this is a no-op (returns nil, false, nil).
// If enabled but missing required env vars, returns an error.
func Setup() (flusher func(), enabled bool, err error) {
	if os.Getenv("LANGFUSE_ENABLED") != "1" {
		return nil, false, nil
	}

	host := os.Getenv("LANGFUSE_HOST")
	pk := os.Getenv("LANGFUSE_PUBLIC_KEY")
	sk := os.Getenv("LANGFUSE_SECRET_KEY")
	if host == "" || pk == "" || sk == "" {
		return nil, false, fmt.Errorf("LANGFUSE_ENABLED=1 but missing LANGFUSE_HOST/PUBLIC_KEY/SECRET_KEY")
	}

	cbh, flusher := langfuse.NewLangfuseHandler(&langfuse.Config{
		Host:      host,
		PublicKey: pk,
		SecretKey: sk,
	})

	callbacks.AppendGlobalHandlers(cbh)
	log.Printf("tracing: Langfuse enabled, host=%q", host)
	return flusher, true, nil
}
