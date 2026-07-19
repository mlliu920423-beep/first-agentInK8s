package httpapi

import (
	"io/fs"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

// NewRouter creates an httprouter.Router with all endpoints registered:
//   - /healthz (k8s probe)
//   - /api/chat (SSE chat)
//   - /api/agents/* (CRUD)
//   - /api/mcp/* (CRUD)
//   - /api/reload (POST)
//   - /* (SPA fallback)
func NewRouter(s *Server, staticFS fs.FS) *httprouter.Router {
	r := httprouter.New()

	// Health probe
	r.GET("/healthz", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		Healthz(w, r)
	})

	// SSE chat endpoint (existing)
	r.GET("/api/chat", s.HandleChat)
	r.POST("/api/chat", s.HandleChat)

	// Agents CRUD
	r.GET("/api/agents", s.HandleListAgents)
	r.GET("/api/agents/:name", s.HandleGetAgent)
	r.POST("/api/agents", s.HandleCreateAgent)
	r.PUT("/api/agents/:name", s.HandleUpdateAgent)
	r.DELETE("/api/agents/:name", s.HandleDeleteAgent)

	// MCP CRUD
	r.GET("/api/mcp", s.HandleListMCPs)
	r.GET("/api/mcp/:name", s.HandleGetMCP)
	r.POST("/api/mcp", s.HandleCreateMCP)
	r.PUT("/api/mcp/:name", s.HandleUpdateMCP)
	r.DELETE("/api/mcp/:name", s.HandleDeleteMCP)

	// Reload
	r.POST("/api/reload", s.HandleReload)

	// SPA fallback (static assets)
	staticHandler := StaticHandler(staticFS)
	r.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		staticHandler.ServeHTTP(w, r)
	})

	return r
}
