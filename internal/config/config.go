package config

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
)

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Addr string
}

// RedisConfig contains Redis connection settings.
type RedisConfig struct {
	Host     string
	Port     int
	Password string
}

// PoppitConfig contains command execution settings.
type PoppitConfig struct {
	Repo                  string
	Branch                string
	Type                  string
	Dir                   string
	Source                string
	NotificationList      string `mapstructure:"notification_list"`
	CommandOutputChannel  string `mapstructure:"command_output_channel"`
	CommandTimeoutSeconds int    `mapstructure:"command_timeout_seconds"`
}

// AllowedTable identifies a dataset/table pair permitted for schema requests.
type AllowedTable struct {
	Dataset string `mapstructure:"dataset"`
	Table   string `mapstructure:"table"`
}

// SchemaConfig contains schema endpoint allowlists.
type SchemaConfig struct {
	AllowedOrigins []string       `mapstructure:"allowed_origins"`
	AllowedTables  []AllowedTable `mapstructure:"allowed_tables"`
}

// Config contains the runtime settings for QueryLab.
type Config struct {
	Server ServerConfig
	Redis  RedisConfig
	Poppit PoppitConfig
	Schema SchemaConfig
}

// Load reads config.yaml, applies defaults, and honors environment overrides.
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("/")

	defaults := map[string]any{
		"server.addr":                    ":8080",
		"redis.host":                     "localhost",
		"redis.port":                     6379,
		"poppit.repo":                    "its-the-vibe/QueryLab",
		"poppit.branch":                  "refs/heads/main",
		"poppit.type":                    "querylab-web",
		"poppit.dir":                     "/tmp",
		"poppit.source":                  "querylab",
		"poppit.notification_list":       "poppit:notifications",
		"poppit.command_output_channel":  "poppit:command-output",
		"poppit.command_timeout_seconds": 30,
	}
	for key, value := range defaults {
		v.SetDefault(key, value)
	}

	environment := map[string]string{
		"server.addr":                    "SERVER_ADDR",
		"redis.host":                     "REDIS_HOST",
		"redis.port":                     "REDIS_PORT",
		"redis.password":                 "REDIS_PASSWORD",
		"poppit.repo":                    "POPPIT_REPO",
		"poppit.branch":                  "POPPIT_BRANCH",
		"poppit.type":                    "POPPIT_TYPE",
		"poppit.dir":                     "POPPIT_DIR",
		"poppit.source":                  "POPPIT_SOURCE",
		"poppit.notification_list":       "POPPIT_SERVICE_REDIS_LIST_NAME",
		"poppit.command_output_channel":  "POPPIT_SERVICE_COMMAND_OUTPUT_CHANNEL",
		"poppit.command_timeout_seconds": "POPPIT_COMMAND_TIMEOUT_SECONDS",
	}
	v.AutomaticEnv()
	for key, name := range environment {
		if err := v.BindEnv(key, name); err != nil {
			return nil, fmt.Errorf("binding environment variable %s: %w", name, err)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		log.Println("No config file found, using defaults and environment variables")
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}
	return &cfg, nil
}
