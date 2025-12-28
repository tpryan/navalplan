package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"app/server"
)

func main() {
	// 1. Basic Configuration
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// 2. Initialize Server
	// We will implement server.New() next, which handles DB connection and Routes
	srv, err := server.New()
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	// 3. Start HTTP Server
	httpServer := &http.Server{
		Addr:    ":" + port,
		Handler: srv.Router,
	}

	// 4. Graceful Shutdown
	go func() {
		fmt.Printf("NavalPlan starting on port %s...\n", port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}
	log.Println("Server exiting")
}
