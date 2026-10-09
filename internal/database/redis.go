package database

import (
	"context"
	"log"

	"github.com/redis/go-redis/v9"
)

var (
	RDB *redis.Client
	Ctx = context.Background()
)

func InitRedis(addr, password string, db int) {
	RDB = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	_, err := RDB.Ping(Ctx).Result()
	if err != nil {
		log.Fatalf("Could not connect to Redis at %s: %v", addr, err)
	}
	log.Printf("Connected to Redis at %s (db %d)", addr, db)
}
