package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/forge/platform/internal/agent"
	"github.com/forge/platform/internal/api"
	"github.com/forge/platform/internal/config"
)

func main() {
	log.Println("Starting Forge Platform...")

	// Load configuration
	cfg := config.Load()
	log.Printf("Port: %d", cfg.Port)

	// Initialize components
	registry := agent.NewRegistry()
	manager := agent.NewManager(registry)

	// Create and start server
	server := api.NewServer(manager, registry)

	// Handle graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Println("Shutting down...")
		if err := server.Shutdown(); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
	}()

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Server listening on %s", addr)
	if err := server.Start(addr); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
