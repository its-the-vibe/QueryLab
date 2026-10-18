package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesDefaultsAndEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SERVER_ADDR", ":9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Errorf("Server.Addr = %q, want %q", cfg.Server.Addr, ":9090")
	}
	if cfg.Redis.Host != "localhost" || cfg.Redis.Port != 6379 {
		t.Errorf("Redis defaults = (%q, %d), want (%q, %d)", cfg.Redis.Host, cfg.Redis.Port, "localhost", 6379)
	}
	if cfg.Poppit.CommandTimeoutSeconds != 30 {
		t.Errorf("Poppit.CommandTimeoutSeconds = %d, want 30", cfg.Poppit.CommandTimeoutSeconds)
	}
}

func TestLoadReadsFileAndEnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("SERVER_ADDR", ":9090")
	t.Setenv("REDIS_PORT", "6380")

	configFile := []byte("server:\n  addr: ':8081'\nredis:\n  host: cache\n  port: 6379\nschema:\n  allowed_tables:\n    - dataset: analytics\n      table: events\n")
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), configFile, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != ":9090" || cfg.Redis.Host != "cache" || cfg.Redis.Port != 6380 {
		t.Errorf("loaded settings = (%q, %q, %d), want (%q, %q, %d)", cfg.Server.Addr, cfg.Redis.Host, cfg.Redis.Port, ":9090", "cache", 6380)
	}
	if len(cfg.Schema.AllowedTables) != 1 || cfg.Schema.AllowedTables[0] != (AllowedTable{Dataset: "analytics", Table: "events"}) {
		t.Errorf("Schema.AllowedTables = %#v, want one analytics.events table", cfg.Schema.AllowedTables)
	}
}
