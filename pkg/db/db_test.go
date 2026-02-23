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

	// Allow override via environment, use smaller pool for tests
	cfg := Config{
		Host:            getEnv("TEST_DB_HOST", "localhost"),
		Port:            5432,
		User:            getEnv("TEST_DB_USER", "benchmark"),
		Password:        getEnv("TEST_DB_PASS", "benchmark_pass"),
		Database:        getEnv("TEST_DB_NAME", "grpc_benchmark"),
		MaxConns:        10,              // Smaller pool for tests
		MinConns:        2,               // Smaller minimum for tests
		MaxConnLifetime: 5 * time.Minute, // Shorter lifetime for tests
		MaxConnIdleTime: time.Minute,     // Shorter idle time for tests
		MaxRetries:      2,               // Fewer retries for faster test failures
		RetryInterval:   50 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Basic connection settings
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

	// Pool settings
	if cfg.MaxConns != 50 {
		t.Errorf("MaxConns = %d, want 50", cfg.MaxConns)
	}
	if cfg.MinConns != 5 {
		t.Errorf("MinConns = %d, want 5", cfg.MinConns)
	}
	if cfg.MaxConnLifetime != time.Hour {
		t.Errorf("MaxConnLifetime = %v, want 1h", cfg.MaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != 30*time.Minute {
		t.Errorf("MaxConnIdleTime = %v, want 30m", cfg.MaxConnIdleTime)
	}

	// Retry settings
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.RetryInterval != 100*time.Millisecond {
		t.Errorf("RetryInterval = %v, want 100ms", cfg.RetryInterval)
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	cfg := Config{
		Host:     "localhost",
		Port:     5432,
		User:     "test",
		Password: "test",
		Database: "test",
		// Leave pool/retry settings as zero
	}

	cfg.applyDefaults()

	if cfg.MaxConns != 50 {
		t.Errorf("MaxConns = %d, want 50 (default)", cfg.MaxConns)
	}
	if cfg.MinConns != 5 {
		t.Errorf("MinConns = %d, want 5 (default)", cfg.MinConns)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3 (default)", cfg.MaxRetries)
	}
}

func TestConfig_ApplyDefaults_PreservesExisting(t *testing.T) {
	cfg := Config{
		Host:            "localhost",
		Port:            5432,
		User:            "test",
		Password:        "test",
		Database:        "test",
		MaxConns:        100,
		MinConns:        10,
		MaxConnLifetime: 2 * time.Hour,
		MaxRetries:      5,
		RetryInterval:   200 * time.Millisecond,
	}

	cfg.applyDefaults()

	// Existing values should be preserved
	if cfg.MaxConns != 100 {
		t.Errorf("MaxConns = %d, want 100 (preserved)", cfg.MaxConns)
	}
	if cfg.MinConns != 10 {
		t.Errorf("MinConns = %d, want 10 (preserved)", cfg.MinConns)
	}
	if cfg.MaxConnLifetime != 2*time.Hour {
		t.Errorf("MaxConnLifetime = %v, want 2h (preserved)", cfg.MaxConnLifetime)
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5 (preserved)", cfg.MaxRetries)
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
