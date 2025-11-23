package main

import (
	"ip2country/config"
	"ip2country/database"
	"ip2country/handlers"
	"ip2country/ratelimit"
	"ip2country/server"
	"log"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	dbFactory := &database.Factory{}
	db, err := dbFactory.NewDatabase(cfg.DatabaseType, cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	rl := ratelimit.NewRateLimiter(cfg.RateLimitRPS)
	defer rl.Stop()

	handler := handlers.NewHandler(db, rl)
	srv := server.SetupServer(cfg, handler)
	server.StartServer(srv, cfg)
	server.WaitForShutdown(srv)
}
