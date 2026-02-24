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
	Client      string // 'go', 'python-grpc', 'python-sdk', 'rust'
	Concurrency int
	DurationSec int
	RateLimit   *int // nullable, for streaming scenarios
	CreatedAt   time.Time

	// Resource usage metrics
	CPUUsageAvg  *float64 // average CPU usage percentage during benchmark
	MemoryMBAvg  *float64 // average memory usage in MB
	MemoryMBPeak *float64 // peak memory usage in MB
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
	Client       string
	Concurrency  int
	DurationSec  int
	TotalSamples int64
	Successful   int64
	P50Latency   float64
	P90Latency   float64
	P99Latency   float64
	AvgLatency   float64
	MinLatency   float64
	MaxLatency   float64
	CPUUsageAvg  *float64
	MemoryMBAvg  *float64
	MemoryMBPeak *float64
}

// StatsFilter defines filter criteria for querying benchmark stats.
type StatsFilter struct {
	Scenario string
	Protocol string
	Client   string
	RunID    *int64
	Limit    int
}

// RecordRun creates a new benchmark run record and returns its ID.
func (db *DB) RecordRun(ctx context.Context, run *BenchmarkRun) (int64, error) {
	var id int64
	client := run.Client
	if client == "" {
		client = "go"
	}
	err := db.Pool.QueryRow(ctx,
		`INSERT INTO benchmark_runs (scenario, protocol, client, concurrency, duration_sec, rate_limit, cpu_usage_avg, memory_mb_avg, memory_mb_peak)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id`,
		run.Scenario, run.Protocol, client, run.Concurrency, run.DurationSec, run.RateLimit,
		run.CPUUsageAvg, run.MemoryMBAvg, run.MemoryMBPeak,
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
		`SELECT run_id, scenario, protocol, client, concurrency, duration_sec,
		        total_samples, successful,
		        p50_latency, p90_latency, p99_latency, avg_latency, min_latency, max_latency,
		        cpu_usage_avg, memory_mb_avg, memory_mb_peak
		 FROM benchmark_stats
		 WHERE run_id = $1`,
		runID,
	).Scan(
		&stats.RunID, &stats.Scenario, &stats.Protocol, &stats.Client, &stats.Concurrency,
		&stats.DurationSec, &stats.TotalSamples, &stats.Successful,
		&stats.P50Latency, &stats.P90Latency, &stats.P99Latency,
		&stats.AvgLatency, &stats.MinLatency, &stats.MaxLatency,
		&stats.CPUUsageAvg, &stats.MemoryMBAvg, &stats.MemoryMBPeak,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get benchmark stats: %w", err)
	}

	return &stats, nil
}

// GetAllStats retrieves stats for all benchmark runs.
func (db *DB) GetAllStats(ctx context.Context) ([]*BenchmarkStats, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT run_id, scenario, protocol, client, concurrency, duration_sec,
		        total_samples, successful,
		        p50_latency, p90_latency, p99_latency, avg_latency, min_latency, max_latency,
		        cpu_usage_avg, memory_mb_avg, memory_mb_peak
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
			&stats.RunID, &stats.Scenario, &stats.Protocol, &stats.Client, &stats.Concurrency,
			&stats.DurationSec, &stats.TotalSamples, &stats.Successful,
			&stats.P50Latency, &stats.P90Latency, &stats.P99Latency,
			&stats.AvgLatency, &stats.MinLatency, &stats.MaxLatency,
			&stats.CPUUsageAvg, &stats.MemoryMBAvg, &stats.MemoryMBPeak,
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

// GetFilteredStats retrieves stats with optional filtering.
func (db *DB) GetFilteredStats(ctx context.Context, filter StatsFilter) ([]*BenchmarkStats, error) {
	query := `SELECT run_id, scenario, protocol, client, concurrency, duration_sec,
	                 total_samples, successful,
	                 p50_latency, p90_latency, p99_latency, avg_latency, min_latency, max_latency,
	                 cpu_usage_avg, memory_mb_avg, memory_mb_peak
	          FROM benchmark_stats
	          WHERE 1=1`

	args := []interface{}{}
	argIdx := 1

	if filter.RunID != nil {
		query += fmt.Sprintf(" AND run_id = $%d", argIdx)
		args = append(args, *filter.RunID)
		argIdx++
	}
	if filter.Scenario != "" {
		query += fmt.Sprintf(" AND scenario = $%d", argIdx)
		args = append(args, filter.Scenario)
		argIdx++
	}
	if filter.Protocol != "" {
		query += fmt.Sprintf(" AND protocol = $%d", argIdx)
		args = append(args, filter.Protocol)
		argIdx++
	}
	if filter.Client != "" {
		query += fmt.Sprintf(" AND client = $%d", argIdx)
		args = append(args, filter.Client)
		argIdx++
	}

	query += " ORDER BY run_id DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, filter.Limit)
	}

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query filtered stats: %w", err)
	}
	defer rows.Close()

	var allStats []*BenchmarkStats
	for rows.Next() {
		var stats BenchmarkStats
		if err := rows.Scan(
			&stats.RunID, &stats.Scenario, &stats.Protocol, &stats.Client, &stats.Concurrency,
			&stats.DurationSec, &stats.TotalSamples, &stats.Successful,
			&stats.P50Latency, &stats.P90Latency, &stats.P99Latency,
			&stats.AvgLatency, &stats.MinLatency, &stats.MaxLatency,
			&stats.CPUUsageAvg, &stats.MemoryMBAvg, &stats.MemoryMBPeak,
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
