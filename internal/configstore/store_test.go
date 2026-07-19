package configstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bigmay/first-agentink8s/internal/agentcfg"
	"github.com/bigmay/first-agentink8s/internal/mcp"
)

func TestAgentsCRUD(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agents-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir, tmpDir) // mcpDir unused for agents test
	if err != nil {
		t.Fatal(err)
	}

	// 1. List should be empty initially
	agents, err := store.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}

	// 2. Create an agent
	newAgent := &agentcfg.AgentConfig{
		Name:        "test_agent",
		Description: "Test agent for CRUD",
		SystemPrompt: "You are Test Agent. Be concise.",
		Tools:       []string{"calculator"},
		MaxStep:     6,
	}
	if err := store.CreateAgent(newAgent); err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	// Check file was created
	if _, err := os.Stat(filepath.Join(tmpDir, "test_agent.yaml")); err != nil {
		t.Errorf("expected yaml file to exist, stat failed: %v", err)
	}

	// 3. Get should return it
	cfg, err := store.GetAgent("test_agent")
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if cfg.Name != "test_agent" {
		t.Errorf("expected name=test_agent, got %s", cfg.Name)
	}
	if cfg.Description != "Test agent for CRUD" {
		t.Errorf("expected description mismatch")
	}

	// 4. Create again should conflict
	if err := store.CreateAgent(newAgent); err != os.ErrExist {
		t.Errorf("expected ErrExist on duplicate create, got %v", err)
	}

	// 5. Update should work
	updatedAgent := &agentcfg.AgentConfig{
		Name:        "test_agent",
		Description: "Updated description",
		SystemPrompt: "You are Updated Test Agent.",
		Tools:       []string{"calculator", "weather"},
		MaxStep:     8,
	}
	if err := store.UpdateAgent("test_agent", updatedAgent); err != nil {
		t.Fatalf("UpdateAgent failed: %v", err)
	}

	// Verify update took effect
	cfg, err = store.GetAgent("test_agent")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Description != "Updated description" {
		t.Errorf("expected updated description, got %q", cfg.Description)
	}
	if len(cfg.Tools) != 2 {
		t.Errorf("expected 2 tools after update, got %d", len(cfg.Tools))
	}

	// 6. Delete should work
	if err := store.DeleteAgent("test_agent"); err != nil {
		t.Fatalf("DeleteAgent failed: %v", err)
	}

	// Get should return not found
	_, err = store.GetAgent("test_agent")
	if err != os.ErrNotExist {
		t.Errorf("expected ErrNotExist after delete, got %v", err)
	}

	// File should be moved to .trash
	trashDir := filepath.Join(tmpDir, ".trash")
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		t.Fatalf("read trash dir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 file in trash after delete, got %d", len(entries))
	}
}

func TestMCPCRUDSimple(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mcp-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir, tmpDir) // agentsDir unused for MCP test
	if err != nil {
		t.Fatal(err)
	}

	// 1. List should be empty initially
	mcps, err := store.ListMCPs()
	if err != nil {
		t.Fatalf("ListMCPs failed: %v", err)
	}
	if len(mcps) != 0 {
		t.Errorf("expected 0 MCPs, got %d", len(mcps))
	}

	// 2. Create an inproc MCP
	newMCP := &mcp.Config{
		Name:        "test_mcp",
		Transport:   "inproc",
		EnabledIf:   "always",
		Provider:    "builtin-demo",
		DefaultRoot: "/tmp",
	}
	if err := store.CreateMCP(newMCP); err != nil {
		t.Fatalf("CreateMCP failed: %v", err)
	}

	// 3. Get should return it
	cfg, err := store.GetMCP("test_mcp")
	if err != nil {
		t.Fatalf("GetMCP failed: %v", err)
	}
	if cfg.Name != "test_mcp" {
		t.Errorf("expected name=test_mcp, got %s", cfg.Name)
	}
	if cfg.Transport != "inproc" {
		t.Errorf("expected transport=inproc, got %s", cfg.Transport)
	}

	// 4. Delete should work
	if err := store.DeleteMCP("test_mcp"); err != nil {
		t.Fatalf("DeleteMCP failed: %v", err)
	}

	_, err = store.GetMCP("test_mcp")
	if err != os.ErrNotExist {
		t.Errorf("expected ErrNotExist after delete, got %v", err)
	}
}

func TestAgentValidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-valid-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Empty name should fail
	invalid1 := &agentcfg.AgentConfig{Name: "", Description: "d", SystemPrompt: "p"}
	if err := store.CreateAgent(invalid1); err == nil {
		t.Error("expected error on empty name")
	}

	// Empty description should fail
	invalid2 := &agentcfg.AgentConfig{Name: "a", Description: "", SystemPrompt: "p"}
	if err := store.CreateAgent(invalid2); err == nil {
		t.Error("expected error on empty description")
	}

	// Empty system prompt should fail
	invalid3 := &agentcfg.AgentConfig{Name: "a", Description: "d", SystemPrompt: ""}
	if err := store.CreateAgent(invalid3); err == nil {
		t.Error("expected error on empty system prompt")
	}
}

func TestUpdateNameMismatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mismatch-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// First create it
	a := &agentcfg.AgentConfig{Name: "foo", Description: "d", SystemPrompt: "p"}
	if err := store.CreateAgent(a); err != nil {
		t.Fatal(err)
	}

	// URL name = "foo" but body name = "bar" should fail
	aWrongName := &agentcfg.AgentConfig{Name: "bar", Description: "d", SystemPrompt: "p"}
	if err := store.UpdateAgent("foo", aWrongName); err == nil {
		t.Error("expected error when name in URL != name in body")
	}
}

func TestAtomicWrite_DoesNotCorruptOnFailure(t *testing.T) {
	// This test is hard to trigger naturally (disk full, etc.)
	// but we can verify that the .tmp file does not remain after
	// a successful write.
	tmpDir, err := os.MkdirTemp("", "atomic-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir, tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	a := &agentcfg.AgentConfig{Name: "atomic", Description: "d", SystemPrompt: "p"}
	if err := store.CreateAgent(a); err != nil {
		t.Fatal(err)
	}

	// No .tmp file should exist
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "atomic.yaml.tmp" {
			t.Error("found leftover .tmp file — atomic write cleanup failed")
		}
	}
}
