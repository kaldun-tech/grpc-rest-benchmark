package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a PostgreSQL connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// Config holds database connection parameters.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string

	// Pool configuration
	MaxConns        int32         // Maximum connections in pool (default: 50)
	MinConns        int32         // Minimum connections to keep open (default: 5)
	MaxConnLifetime time.Duration // Max lifetime of a connection (default: 1 hour)
	MaxConnIdleTime time.Duration // Max idle time before closing (default: 30 min)

	// Retry configuration
	MaxRetries    int           // Max connection retries (default: 3)
	RetryInterval time.Duration // Initial retry interval (default: 100ms)
}

// DefaultConfig returns the default config for local development.
func DefaultConfig() Config {
	return Config{
		Host:            "localhost",
		Port:            5432,
		User:            "benchmark",
		Password:        "benchmark_pass",
		Database:        "grpc_benchmark",
		MaxConns:        50,
		MinConns:        5,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
		MaxRetries:      3,
		RetryInterval:   100 * time.Millisecond,
	}
}

// ConnString builds a PostgreSQL connection string from config.
func (c Config) ConnString() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.User, c.Password, c.Host, c.Port, c.Database,
	)
}

// applyDefaults fills in zero values with defaults.
func (c *Config) applyDefaults() {
	if c.MaxConns == 0 {
		c.MaxConns = 50
	}
	if c.MinConns == 0 {
		c.MinConns = 5
	}
	if c.MaxConnLifetime == 0 {
		c.MaxConnLifetime = time.Hour
	}
	if c.MaxConnIdleTime == 0 {
		c.MaxConnIdleTime = 30 * time.Minute
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
	if c.RetryInterval == 0 {
		c.RetryInterval = 100 * time.Millisecond
	}
}

// New creates a new database connection pool with retry logic.
func New(ctx context.Context, cfg Config) (*DB, error) {
	cfg.applyDefaults()

	// Configure pool
	poolCfg, err := pgxpool.ParseConfig(cfg.ConnString())
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime

	// Retry loop with exponential backoff
	var pool *pgxpool.Pool
	var lastErr error
	retryInterval := cfg.RetryInterval

	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryInterval):
				retryInterval *= 2 // Exponential backoff
			}
		}

		pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			lastErr = err
			continue
		}

		// Verify connectivity
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			lastErr = err
			continue
		}

		return &DB{Pool: pool}, nil
	}

	return nil, fmt.Errorf("failed to connect after %d retries: %w", cfg.MaxRetries, lastErr)
}

// Close closes the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}
