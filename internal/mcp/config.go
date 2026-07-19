// Package mcp is the declarative loader for MCP servers.
//
// Each *.yaml file in the configured directory (env `MCP_DIR`, default `mcp`)
// declares one MCP server: what transport, how to start it, and under what
// condition it should be enabled at startup.
//
// The loader:
//  1. Scans mcp/*.yaml (deterministic, dictionary-order)
//  2. Evaluates each file's `enabled_if` expression
//  3. Dispatches enabled entries to the registered Driver for that transport
//  4. Collects io.Closer handles so main.go can shut them down cleanly
//
// A Driver is a transport-level primitive (inproc / stdio / future sse / http);
// see driver.go. New MCP servers of an existing transport = new yaml file.
// New transports = new driver implementation.
package mcp

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is one MCP server declaration parsed from mcp/<name>.yaml.
//
// Fields marked (inproc only) / (stdio only) are honoured only by that driver;
// setting them on the wrong transport is a parse-time error (see validate).
type Config struct {
	Name      string `yaml:"name"`
	Transport string `yaml:"transport"` // "inproc" | "stdio"

	// EnabledIf declares when this server should be started. Grammar (MVP):
	//   "" | "always"        → always enabled
	//   "env:VAR"            → os.Getenv("VAR") != ""
	//   "env:VAR=value"      → os.Getenv("VAR") == "value"
	// Anything else is a parse-time error (typos fail-fast rather than
	// silently disable the server, which would be much harder to debug).
	EnabledIf string `yaml:"enabled_if,omitempty"`

	// inproc only ---
	Provider    string `yaml:"provider,omitempty"`     // which builtin implementation
	DefaultRoot string `yaml:"default_root,omitempty"` // used e.g. by list_dir when caller passes ""

	// stdio only ---
	Command     string            `yaml:"command,omitempty"`
	Args        []string          `yaml:"args,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	InitTimeout time.Duration     `yaml:"init_timeout,omitempty"` // default 30s if zero

	// path retained for error messages; not written from yaml.
	sourcePath string `yaml:"-"`
}

// SourcePath returns the mcp/*.yaml path this Config came from (for logs).
func (c *Config) SourcePath() string { return c.sourcePath }

// ValidateForAPI checks that the Config is well-formed: required fields are
// present, transport-specific fields are correctly scoped, enabled_if
// grammar is valid. This is the same validation that runs at boot time.
func (c *Config) ValidateForAPI() error {
	return c.validate()
}

// ParseConfigForAPI parses raw yaml bytes into a Config, applies ${env:VAR}
// expansion, and runs validation. This is exactly what happens at boot time
// in the loader.
func ParseConfigForAPI(sourcePath string, raw []byte) (Config, error) {
	return parseConfig(sourcePath, raw)
}

// parseConfig reads a single yaml file into a Config, applies expansion of
// ${env:VAR} / ${env:VAR:-default} references in stringy fields, and
// validates the result. `enabled_if` is NOT evaluated here — that happens
// in the loader so we can log a nice "disabled" message per file.
func parseConfig(path string, raw []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.sourcePath = path

	// Expand ${env:VAR[:-default]} everywhere strings can appear.
	cfg.Command = expandVars(cfg.Command)
	cfg.DefaultRoot = expandVars(cfg.DefaultRoot)
	for i, a := range cfg.Args {
		cfg.Args[i] = expandVars(a)
	}
	for k, v := range cfg.Env {
		cfg.Env[k] = expandVars(v)
	}

	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// validate rejects malformed configs at load time so problems surface as pod
// crashloop, not as mysterious runtime tool-lookup failures.
func (c *Config) validate() error {
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch c.Transport {
	case "inproc":
		if c.Provider == "" {
			return fmt.Errorf("inproc transport requires `provider`")
		}
		if c.Command != "" || len(c.Args) > 0 || len(c.Env) > 0 {
			return fmt.Errorf("inproc transport must not set command/args/env")
		}
	case "stdio":
		if c.Command == "" {
			return fmt.Errorf("stdio transport requires `command`")
		}
		if c.Provider != "" || c.DefaultRoot != "" {
			return fmt.Errorf("stdio transport must not set provider/default_root")
		}
	case "":
		return fmt.Errorf("transport is required (`inproc` or `stdio`)")
	default:
		return fmt.Errorf("unknown transport %q (want `inproc` or `stdio`)", c.Transport)
	}
	// enabled_if grammar is checked eagerly so typos crash boot.
	if _, err := parseCondition(c.EnabledIf); err != nil {
		return fmt.Errorf("enabled_if: %w", err)
	}
	return nil
}

// InitTimeoutOrDefault returns the configured timeout or 30s (matches the
// legacy hardcoded value in the pre-refactor filesystem.go, so migration is
// runtime-behavior-preserving; see ADR-005).
func (c *Config) InitTimeoutOrDefault() time.Duration {
	if c.InitTimeout <= 0 {
		return 30 * time.Second
	}
	return c.InitTimeout
}
