package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cloudwego/eino/flow/agent/multiagent/host"
	"github.com/julienschmidt/httprouter"

	"github.com/bigmay/first-agentink8s/internal/agentcfg"
	"github.com/bigmay/first-agentink8s/internal/configstore"
	"github.com/bigmay/first-agentink8s/internal/mcp"
)

// Server owns all HTTP endpoints: SSE chat + CRUD config + static assets.
type Server struct {
	Sup   Supervised
	Store *configstore.Store
}

// Supervised is the interface for what httpapi needs from a Supervisor —
// it lets tests use a fake supervisor without bringing up the full Eino
// stack.
type Supervised interface {
	Current() *host.MultiAgent
	Reload(ctx context.Context) error
}

// JSON response types --------------------------------------------------------

type simpleResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type reloadResponse struct {
	Status      string  `json:"status"`
	TookMs      float64 `json:"took_ms"`
	Message     string  `json:"message,omitempty"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// HTTP helpers ----------------------------------------------------------------

func sendJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func sendError(w http.ResponseWriter, status int, msg, details string) {
	sendJSON(w, status, errorResponse{Error: msg, Details: details})
}

// Agents CRUD -----------------------------------------------------------------

// HandleListAgents GET /api/agents
func (s *Server) HandleListAgents(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	agents, err := s.Store.ListAgents()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to list agents", err.Error())
		return
	}
	sendJSON(w, http.StatusOK, agents)
}

// HandleGetAgent GET /api/agents/:name
func (s *Server) HandleGetAgent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")
	cfg, err := s.Store.GetAgent(name)
	if errors.Is(err, os.ErrNotExist) {
		sendError(w, http.StatusNotFound, "not found", fmt.Sprintf("agent %q does not exist", name))
		return
	}
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to get agent", err.Error())
		return
	}
	sendJSON(w, http.StatusOK, cfg)
}

// HandleCreateAgent POST /api/agents
func (s *Server) HandleCreateAgent(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var cfg agentcfg.AgentConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		sendError(w, http.StatusBadRequest, "invalid json", err.Error())
		return
	}

	if err := s.Store.CreateAgent(&cfg); errors.Is(err, os.ErrExist) {
		sendError(w, http.StatusConflict, "already exists", fmt.Sprintf("agent %q already exists", cfg.Name))
		return
	} else if err != nil {
		sendError(w, http.StatusBadRequest, "validation failed", err.Error())
		return
	}

	// Auto-reload to pick up the new agent
	start := time.Now()
	if err := s.Sup.Reload(r.Context()); err != nil {
		sendError(w, http.StatusInternalServerError, "agent created but reload failed", err.Error())
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/api/agents/%s", cfg.Name))
	sendJSON(w, http.StatusCreated, simpleResponse{
		Status:  "created",
		Message: fmt.Sprintf("agent %q created, reload took %.1fms", cfg.Name, float64(time.Since(start).Microseconds())/1000),
	})
}

// HandleUpdateAgent PUT /api/agents/:name
func (s *Server) HandleUpdateAgent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")
	var cfg agentcfg.AgentConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		sendError(w, http.StatusBadRequest, "invalid json", err.Error())
		return
	}

	if err := s.Store.UpdateAgent(name, &cfg); errors.Is(err, os.ErrNotExist) {
		sendError(w, http.StatusNotFound, "not found", fmt.Sprintf("agent %q does not exist", name))
		return
	} else if err != nil {
		sendError(w, http.StatusBadRequest, "validation failed", err.Error())
		return
	}

	// Auto-reload
	start := time.Now()
	if err := s.Sup.Reload(r.Context()); err != nil {
		sendError(w, http.StatusInternalServerError, "config updated but reload failed", err.Error())
		return
	}

	sendJSON(w, http.StatusOK, simpleResponse{
		Status:  "updated",
		Message: fmt.Sprintf("agent %q updated, reload took %.1fms", name, float64(time.Since(start).Microseconds())/1000),
	})
}

// HandleDeleteAgent DELETE /api/agents/:name
func (s *Server) HandleDeleteAgent(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")
	if err := s.Store.DeleteAgent(name); errors.Is(err, os.ErrNotExist) {
		sendError(w, http.StatusNotFound, "not found", fmt.Sprintf("agent %q does not exist", name))
		return
	} else if err != nil {
		sendError(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}

	// Auto-reload
	start := time.Now()
	if err := s.Sup.Reload(r.Context()); err != nil {
		sendError(w, http.StatusInternalServerError, "agent deleted but reload failed", err.Error())
		return
	}

	sendJSON(w, http.StatusOK, simpleResponse{
		Status:  "deleted",
		Message: fmt.Sprintf("agent %q deleted (moved to trash), reload took %.1fms", name, float64(time.Since(start).Microseconds())/1000),
	})
}

// MCP CRUD --------------------------------------------------------------------

// HandleListMCPs GET /api/mcp
func (s *Server) HandleListMCPs(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	mcps, err := s.Store.ListMCPs()
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to list MCP servers", err.Error())
		return
	}
	sendJSON(w, http.StatusOK, mcps)
}

// HandleGetMCP GET /api/mcp/:name
func (s *Server) HandleGetMCP(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")
	cfg, err := s.Store.GetMCP(name)
	if errors.Is(err, os.ErrNotExist) {
		sendError(w, http.StatusNotFound, "not found", fmt.Sprintf("MCP %q does not exist", name))
		return
	}
	if err != nil {
		sendError(w, http.StatusInternalServerError, "failed to get MCP", err.Error())
		return
	}
	sendJSON(w, http.StatusOK, cfg)
}

// HandleCreateMCP POST /api/mcp
func (s *Server) HandleCreateMCP(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var cfg mcp.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		sendError(w, http.StatusBadRequest, "invalid json", err.Error())
		return
	}

	if err := s.Store.CreateMCP(&cfg); errors.Is(err, os.ErrExist) {
		sendError(w, http.StatusConflict, "already exists", fmt.Sprintf("MCP %q already exists", cfg.Name))
		return
	} else if err != nil {
		sendError(w, http.StatusBadRequest, "validation failed", err.Error())
		return
	}

	// Auto-reload
	start := time.Now()
	if err := s.Sup.Reload(r.Context()); err != nil {
		sendError(w, http.StatusInternalServerError, "MCP created but reload failed", err.Error())
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/api/mcp/%s", cfg.Name))
	sendJSON(w, http.StatusCreated, simpleResponse{
		Status:  "created",
		Message: fmt.Sprintf("MCP %q created, reload took %.1fms", cfg.Name, float64(time.Since(start).Microseconds())/1000),
	})
}

// HandleUpdateMCP PUT /api/mcp/:name
func (s *Server) HandleUpdateMCP(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")
	var cfg mcp.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		sendError(w, http.StatusBadRequest, "invalid json", err.Error())
		return
	}

	if err := s.Store.UpdateMCP(name, &cfg); errors.Is(err, os.ErrNotExist) {
		sendError(w, http.StatusNotFound, "not found", fmt.Sprintf("MCP %q does not exist", name))
		return
	} else if err != nil {
		sendError(w, http.StatusBadRequest, "validation failed", err.Error())
		return
	}

	// Auto-reload
	start := time.Now()
	if err := s.Sup.Reload(r.Context()); err != nil {
		sendError(w, http.StatusInternalServerError, "config updated but reload failed", err.Error())
		return
	}

	sendJSON(w, http.StatusOK, simpleResponse{
		Status:  "updated",
		Message: fmt.Sprintf("MCP %q updated, reload took %.1fms", name, float64(time.Since(start).Microseconds())/1000),
	})
}

// HandleDeleteMCP DELETE /api/mcp/:name
func (s *Server) HandleDeleteMCP(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")
	if err := s.Store.DeleteMCP(name); errors.Is(err, os.ErrNotExist) {
		sendError(w, http.StatusNotFound, "not found", fmt.Sprintf("MCP %q does not exist", name))
		return
	} else if err != nil {
		sendError(w, http.StatusInternalServerError, "delete failed", err.Error())
		return
	}

	// Auto-reload
	start := time.Now()
	if err := s.Sup.Reload(r.Context()); err != nil {
		sendError(w, http.StatusInternalServerError, "MCP deleted but reload failed", err.Error())
		return
	}

	sendJSON(w, http.StatusOK, simpleResponse{
		Status:  "deleted",
		Message: fmt.Sprintf("MCP %q deleted (moved to trash), reload took %.1fms", name, float64(time.Since(start).Microseconds())/1000),
	})
}

// Reload ----------------------------------------------------------------------

// HandleReload POST /api/reload
func (s *Server) HandleReload(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	start := time.Now()
	if err := s.Sup.Reload(r.Context()); err != nil {
		sendError(w, http.StatusInternalServerError, "reload failed", err.Error())
		return
	}
	sendJSON(w, http.StatusOK, reloadResponse{
		Status: "reloaded",
		TookMs: float64(time.Since(start).Microseconds()) / 1000,
	})
}
