package main

import (
	"log"
	"os"

	"github.com/farbodsalimi/genevieve/internal/storage"
	"github.com/redis/go-redis/v9"
)

// pickStore selects the storage backend from the environment.
func pickStore() storage.Store {
	if os.Getenv("STORE") == "redis" {
		addr := os.Getenv("REDIS_ADDR")
		if addr == "" {
			addr = "localhost:6379"
		}
		log.Printf("using redis store at %s", addr)
		return storage.NewRedisStore(redis.NewClient(&redis.Options{Addr: addr}))
	}
	log.Print("using in-memory store")
	return storage.NewMemoryStore()
}
