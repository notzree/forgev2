package api

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/forge/platform/internal/agent"
)

// Server is the HTTP server
type Server struct {
	echo     *echo.Echo
	handlers *Handlers
	proxy    *Proxy
}

// NewServer creates a new HTTP server
func NewServer(manager *agent.Manager, registry *agent.Registry) *Server {
	handlers := NewHandlers(manager, registry)
	proxy := NewProxy(manager)

	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Health checks
	e.GET("/healthz", handlers.Healthz)
	e.GET("/readyz", handlers.Readyz)

	// API routes
	api := e.Group("/api/v1")

	// Agent management
	agents := api.Group("/agents")
	agents.POST("", handlers.CreateAgent)
	agents.GET("", handlers.ListAgents)
	agents.GET("/:id", handlers.GetAgent)
	agents.DELETE("/:id", handlers.DeleteAgent)
	agents.GET("/:id/messages", handlers.GetMessages)

	// WebSocket endpoint for streaming
	agents.GET("/:id/stream", proxy.HandleWebSocket)

	return &Server{
		echo:     e,
		handlers: handlers,
		proxy:    proxy,
	}
}

// Start starts the HTTP server
func (s *Server) Start(addr string) error {
	return s.echo.Start(addr)
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown() error {
	return s.echo.Close()
}
