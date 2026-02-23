package db

import (
	"context"
	"os"
	"testing"
	"time"
)

// testDB creates a test database connection.
// Skips the test if database is not available.
func testDB(t *testing.T) *DB {
	t.Helper()

	// Allow override via environment
	cfg := Config{
		Host:     getEnv("TEST_DB_HOST", "localhost"),
		Port:     5432,
		User:     getEnv("TEST_DB_USER", "benchmark"),
		Password: getEnv("TEST_DB_PASS", "benchmark_pass"),
		Database: getEnv("TEST_DB_NAME", "grpc_benchmark"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := New(ctx, cfg)
	if err != nil {
		t.Skipf("Skipping test: database not available: %v", err)
	}

	return db
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func TestConfig_ConnString(t *testing.T) {
	cfg := Config{
		Host:     "localhost",
		Port:     5432,
		User:     "testuser",
		Password: "testpass",
		Database: "testdb",
	}

	expected := "postgres://testuser:testpass@localhost:5432/testdb?sslmode=disable"
	got := cfg.ConnString()

	if got != expected {
		t.Errorf("ConnString() = %q, want %q", got, expected)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want %q", cfg.Host, "localhost")
	}
	if cfg.Port != 5432 {
		t.Errorf("Port = %d, want %d", cfg.Port, 5432)
	}
	if cfg.User != "benchmark" {
		t.Errorf("User = %q, want %q", cfg.User, "benchmark")
	}
	if cfg.Database != "grpc_benchmark" {
		t.Errorf("Database = %q, want %q", cfg.Database, "grpc_benchmark")
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	cfg := Config{
		Host:     "nonexistent-host-12345",
		Port:     5432,
		User:     "invalid",
		Password: "invalid",
		Database: "invalid",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := New(ctx, cfg)
	if err == nil {
		t.Error("New() expected error for invalid config, got nil")
	}
}
