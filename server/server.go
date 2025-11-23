package server

import (
	"context"
	"ip2country/config"
	"ip2country/handlers"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func SetupServer(cfg *config.Config, handler *handlers.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/find-country", handler.FindCountry)

	return &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

func StartServer(server *http.Server, cfg *config.Config) {
	go func() {
		log.Printf("Server starting on port %s", cfg.Port)
		log.Printf("Rate limit: %d requests per second", cfg.RateLimitRPS)
		log.Printf("Database type: %s, path: %s", cfg.DatabaseType, cfg.DatabasePath)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()
}

func WaitForShutdown(server *http.Server) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

