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
	db, err := dbFactory.NewDatabase(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	rl := ratelimit.NewRateLimiterWithRedis(cfg.RateLimitRPS, cfg.RedisURL)
	defer rl.Stop()

	handler := handlers.NewHandler(db)
	srv := server.SetupServer(cfg, handler, rl)
	server.StartServer(srv, cfg)
	server.WaitForShutdown(srv)
}
