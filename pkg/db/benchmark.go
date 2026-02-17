package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// BenchmarkRun represents a benchmark run record.
type BenchmarkRun struct {
	ID          int64
	Scenario    string // 'balance_query', 'tx_stream'
	Protocol    string // 'rest', 'grpc'
	Concurrency int
	DurationSec int
	RateLimit   *int // nullable, for streaming scenarios
	CreatedAt   time.Time
}

// BenchmarkSample represents a single request latency sample.
type BenchmarkSample struct {
	ID        int64
	RunID     int64
	LatencyMs float64
	Success   bool
	ErrorType *string // nullable
	Timestamp time.Time
}

// BenchmarkStats represents aggregated stats for a run.
type BenchmarkStats struct {
	RunID        int64
	Scenario     string
	Protocol     string
	Concurrency  int
	TotalSamples int64
	Successful   int64
	P50Latency   float64
	P90Latency   float64
	P99Latency   float64
	AvgLatency   float64
	MinLatency   float64
	MaxLatency   float64
}

// RecordRun creates a new benchmark run record and returns its ID.
func (db *DB) RecordRun(ctx context.Context, run *BenchmarkRun) (int64, error) {
	var id int64
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO benchmark_runs (scenario, protocol, concurrency, duration_sec, rate_limit)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		run.Scenario, run.Protocol, run.Concurrency, run.DurationSec, run.RateLimit,
	).Scan(&id)

	if err != nil {
		return 0, fmt.Errorf("failed to record benchmark run: %w", err)
	}

	return id, nil
}

// RecordSample records a single latency sample for a benchmark run.
func (db *DB) RecordSample(ctx context.Context, sample *BenchmarkSample) error {
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO benchmark_samples (run_id, latency_ms, success, error_type, timestamp)
		 VALUES ($1, $2, $3, $4, $5)`,
		sample.RunID, sample.LatencyMs, sample.Success, sample.ErrorType, sample.Timestamp,
	)

	if err != nil {
		return fmt.Errorf("failed to record benchmark sample: %w", err)
	}

	return nil
}

// RecordSamples records multiple latency samples using PostgreSQL COPY protocol.
func (db *DB) RecordSamples(ctx context.Context, samples []*BenchmarkSample) error {
	if len(samples) == 0 {
		return nil
	}

	// Build rows for COPY
	rows := make([][]interface{}, len(samples))
	for i, sample := range samples {
		rows[i] = []interface{}{
			sample.RunID,
			sample.LatencyMs,
			sample.Success,
			sample.ErrorType,
			sample.Timestamp,
		}
	}

	// Use COPY protocol for fast bulk insert
	copied, err := db.Pool.CopyFrom(
		ctx,
		pgx.Identifier{"benchmark_samples"},
		[]string{"run_id", "latency_ms", "success", "error_type", "timestamp"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("failed to copy samples: %w", err)
	}

	if copied != int64(len(samples)) {
		return fmt.Errorf("expected to copy %d rows, copied %d", len(samples), copied)
	}

	return nil
}

// GetStats retrieves aggregated statistics for a benchmark run.
func (db *DB) GetStats(ctx context.Context, runID int64) (*BenchmarkStats, error) {
	var stats BenchmarkStats
	err := db.Pool.QueryRow(ctx,
		`SELECT run_id, scenario, protocol, concurrency, total_samples, successful,
		        p50_latency, p90_latency, p99_latency, avg_latency, min_latency, max_latency
		 FROM benchmark_stats
		 WHERE run_id = $1`,
		runID,
	).Scan(
		&stats.RunID, &stats.Scenario, &stats.Protocol, &stats.Concurrency,
		&stats.TotalSamples, &stats.Successful,
		&stats.P50Latency, &stats.P90Latency, &stats.P99Latency,
		&stats.AvgLatency, &stats.MinLatency, &stats.MaxLatency,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get benchmark stats: %w", err)
	}

	return &stats, nil
}

// GetAllStats retrieves stats for all benchmark runs.
func (db *DB) GetAllStats(ctx context.Context) ([]*BenchmarkStats, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT run_id, scenario, protocol, concurrency, total_samples, successful,
		        p50_latency, p90_latency, p99_latency, avg_latency, min_latency, max_latency
		 FROM benchmark_stats
		 ORDER BY run_id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query benchmark stats: %w", err)
	}
	defer rows.Close()

	var allStats []*BenchmarkStats
	for rows.Next() {
		var stats BenchmarkStats
		if err := rows.Scan(
			&stats.RunID, &stats.Scenario, &stats.Protocol, &stats.Concurrency,
			&stats.TotalSamples, &stats.Successful,
			&stats.P50Latency, &stats.P90Latency, &stats.P99Latency,
			&stats.AvgLatency, &stats.MinLatency, &stats.MaxLatency,
		); err != nil {
			return nil, fmt.Errorf("failed to scan stats row: %w", err)
		}
		allStats = append(allStats, &stats)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating stats rows: %w", err)
	}

	return allStats, nil
}
