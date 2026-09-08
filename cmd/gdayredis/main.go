package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

type Config struct {
	Redis struct {
		Host     string
		Port     int
		Password string
	}
	PingIntervalSeconds int `mapstructure:"ping_interval_seconds"`
}

func loadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/")

	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("ping_interval_seconds", 5)

	// Allow REDIS_PASSWORD from environment / .env
	viper.AutomaticEnv()
	viper.BindEnv("redis.password", "REDIS_PASSWORD")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		log.Println("No config file found, using defaults and environment variables")
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}
	return &cfg, nil
}

func main() {
	fmt.Println("Gday World")

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Redis.Password,
	})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	interval := time.Duration(cfg.PingIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Pinging Redis at %s every %v", addr, interval)

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down")
			return
		case <-ticker.C:
			if err := rdb.Ping(ctx).Err(); err != nil {
				log.Printf("Redis ping failed: %v", err)
			} else {
				// log.Println("Redis ping: PONG")
			}
		}
	}
}
