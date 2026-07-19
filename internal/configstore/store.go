// Package configstore handles reading/writing agent and MCP configuration to disk.
//
// All write operations are atomic (write .tmp then rename) to avoid half-written
// files. Delete operations are soft-delete (move to .trash/ with timestamp) so
// users can manually recover from mistakes.
//
// The store is concurrency-safe (protected by a single mutex). However, it only
// guards against concurrent writes *within the same process* — if two separate
// processes write to the same files, you can still get races. That's considered
// out of scope for the single-user local development scenario.
package configstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bigmay/first-agentink8s/internal/agentcfg"
	"github.com/bigmay/first-agentink8s/internal/mcp"
)

// Store reads and writes agent/MCP yaml files to/from disk.
//
// Zero value is not usable; use NewStore().
type Store struct {
	agentsDir string // e.g. "agents/" or os.Getenv("AGENTS_DIR")
	mcpDir    string // e.g. "mcp/" or os.Getenv("MCP_DIR")
	mu        sync.Mutex
}

// NewStore creates a config store rooted at the given directories.
// Directories are created if they don't exist.
func NewStore(agentsDir, mcpDir string) (*Store, error) {
	if err := os.MkdirAll(agentsDir, 0750); err != nil {
		return nil, fmt.Errorf("create agents dir %q: %w", agentsDir, err)
	}
	if err := os.MkdirAll(mcpDir, 0750); err != nil {
		return nil, fmt.Errorf("create mcp dir %q: %w", mcpDir, err)
	}
	// .trash subdirs are created lazily on first delete
	return &Store{
		agentsDir: agentsDir,
		mcpDir:    mcpDir,
	}, nil
}

// ---------------------------------------------------------------------------
// Agents CRUD
// ---------------------------------------------------------------------------

// ListAgents returns all agent configs, sorted by name.
func (s *Store) ListAgents() ([]agentcfg.AgentConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.listAgentsLocked()
}

func (s *Store) listAgentsLocked() ([]agentcfg.AgentConfig, error) {
	entries, err := os.ReadDir(s.agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir %q: %w", s.agentsDir, err)
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".yaml" || ext == ".yml" {
			paths = append(paths, filepath.Join(s.agentsDir, e.Name()))
		}
	}
	sort.Strings(paths)

	var out []agentcfg.AgentConfig
	seenName := map[string]string{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		var cfg agentcfg.AgentConfig
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		if cfg.Name == "" {
			return nil, fmt.Errorf("%s: name is required", p)
		}
		if prev, ok := seenName[cfg.Name]; ok {
			return nil, fmt.Errorf("duplicate agent name %q in %s (also in %s)", cfg.Name, p, prev)
		}
		seenName[cfg.Name] = p
		out = append(out, cfg)
	}
	return out, nil
}

// GetAgent returns a single agent config by name.
// Returns os.ErrNotExist if no such agent.
func (s *Store) GetAgent(name string) (*agentcfg.AgentConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.listAgentsLocked()
	if err != nil {
		return nil, err
	}
	for _, cfg := range all {
		if cfg.Name == name {
			return &cfg, nil
		}
	}
	return nil, os.ErrNotExist
}

// CreateAgent writes a new agent yaml file.
// Returns os.ErrExist if an agent with that name already exists.
func (s *Store) CreateAgent(cfg *agentcfg.AgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for existing
	path := filepath.Join(s.agentsDir, cfg.Name+".yaml")
	if _, err := os.Stat(path); err == nil {
		return os.ErrExist
	}

	// Validate (same as agentcfg.loadOne checks)
	if cfg.Name == "" {
		return fmt.Errorf("name is required")
	}
	if cfg.Description == "" {
		return fmt.Errorf("description is required")
	}
	if cfg.SystemPrompt == "" {
		return fmt.Errorf("system_prompt is required")
	}
	if cfg.MaxStep <= 0 {
		cfg.MaxStep = 12
	}

	return s.writeYamlAtomic(path, cfg)
}

// UpdateAgent overwrites an existing agent yaml file.
// Returns os.ErrNotExist if no such agent.
func (s *Store) UpdateAgent(name string, cfg *agentcfg.AgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Name in URL must match name in body
	if cfg.Name != name {
		return fmt.Errorf("name in body (%q) does not match name in URL (%q)", cfg.Name, name)
	}

	// Check it exists
	path := filepath.Join(s.agentsDir, name+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.ErrNotExist
	}

	// Validate (same as CreateAgent)
	if cfg.Description == "" {
		return fmt.Errorf("description is required")
	}
	if cfg.SystemPrompt == "" {
		return fmt.Errorf("system_prompt is required")
	}
	if cfg.MaxStep <= 0 {
		cfg.MaxStep = 12
	}

	return s.writeYamlAtomic(path, cfg)
}

// DeleteAgent soft-deletes an agent: moves it to .trash/ with a timestamp.
// Returns os.ErrNotExist if no such agent.
func (s *Store) DeleteAgent(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.agentsDir, name+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.ErrNotExist
	}

	trashDir := filepath.Join(s.agentsDir, ".trash")
	if err := os.MkdirAll(trashDir, 0750); err != nil {
		return fmt.Errorf("create trash dir: %w", err)
	}

	ts := time.Now().Format("20060102-150405.000")
	trashPath := filepath.Join(trashDir, fmt.Sprintf("%s.%s.yaml", name, ts))
	if err := os.Rename(path, trashPath); err != nil {
		return fmt.Errorf("move to trash: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// MCP CRUD
// ---------------------------------------------------------------------------

// ListMCPs returns all MCP server configs, sorted by name.
func (s *Store) ListMCPs() ([]mcp.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.listMCPsLocked()
}

func (s *Store) listMCPsLocked() ([]mcp.Config, error) {
	entries, err := os.ReadDir(s.mcpDir)
	if err != nil {
		return nil, fmt.Errorf("read mcp dir %q: %w", s.mcpDir, err)
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext == ".yaml" || ext == ".yml" {
			paths = append(paths, filepath.Join(s.mcpDir, e.Name()))
		}
	}
	sort.Strings(paths)

	var out []mcp.Config
	seenName := map[string]string{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		// Use the same parse + validate + expand logic as mcp.LoadAll
		cfg, err := mcp.ParseConfigForAPI(p, raw)
		if err != nil {
			return nil, err
		}
		if prev, ok := seenName[cfg.Name]; ok {
			return nil, fmt.Errorf("duplicate MCP name %q in %s (also in %s)", cfg.Name, p, prev)
		}
		seenName[cfg.Name] = p
		out = append(out, cfg)
	}
	return out, nil
}

// GetMCP returns a single MCP server config by name.
// Returns os.ErrNotExist if no such MCP.
func (s *Store) GetMCP(name string) (*mcp.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.listMCPsLocked()
	if err != nil {
		return nil, err
	}
	for _, cfg := range all {
		if cfg.Name == name {
			return &cfg, nil
		}
	}
	return nil, os.ErrNotExist
}

// CreateMCP writes a new MCP yaml file.
// Returns os.ErrExist if an MCP with that name already exists.
func (s *Store) CreateMCP(cfg *mcp.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.mcpDir, cfg.Name+".yaml")
	if _, err := os.Stat(path); err == nil {
		return os.ErrExist
	}

	// Validate using mcp.Config.Validate()
	if err := cfg.ValidateForAPI(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	return s.writeYamlAtomic(path, cfg)
}

// UpdateMCP overwrites an existing MCP yaml file.
// Returns os.ErrNotExist if no such MCP.
func (s *Store) UpdateMCP(name string, cfg *mcp.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Name in URL must match name in body
	if cfg.Name != name {
		return fmt.Errorf("name in body (%q) does not match name in URL (%q)", cfg.Name, name)
	}

	path := filepath.Join(s.mcpDir, name+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.ErrNotExist
	}

	// Validate
	if err := cfg.ValidateForAPI(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	return s.writeYamlAtomic(path, cfg)
}

// DeleteMCP soft-deletes an MCP server: moves it to .trash/ with a timestamp.
// Returns os.ErrNotExist if no such MCP.
func (s *Store) DeleteMCP(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.mcpDir, name+".yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.ErrNotExist
	}

	trashDir := filepath.Join(s.mcpDir, ".trash")
	if err := os.MkdirAll(trashDir, 0750); err != nil {
		return fmt.Errorf("create trash dir: %w", err)
	}

	ts := time.Now().Format("20060102-150405.000")
	trashPath := filepath.Join(trashDir, fmt.Sprintf("%s.%s.yaml", name, ts))
	if err := os.Rename(path, trashPath); err != nil {
		return fmt.Errorf("move to trash: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeYamlAtomic writes cfg to path atomically: first write to a .tmp file,
// then rename over the original. Rename is atomic on POSIX and NTFS.
func (s *Store) writeYamlAtomic(path string, cfg interface{}) error {
	tmpPath := path + ".tmp"

	// Marshal with indentation for human readability
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	// Write tmp file (0600 = user rw only)
	if err := os.WriteFile(tmpPath, b, 0600); err != nil {
		_ = os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("write tmp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) // best-effort cleanup
		return fmt.Errorf("atomic rename: %w", err)
	}

	return nil
}
