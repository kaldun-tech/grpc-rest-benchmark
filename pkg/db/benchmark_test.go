package db

import (
	"context"
	"testing"
	"time"
)

func TestRecordRun(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cpuAvg := 50.5
	memAvg := 100.0
	memPeak := 150.0

	run := &BenchmarkRun{
		Scenario:     "balance",
		Protocol:     "grpc",
		Client:       "go-test",
		Concurrency:  10,
		DurationSec:  30,
		CPUUsageAvg:  &cpuAvg,
		MemoryMBAvg:  &memAvg,
		MemoryMBPeak: &memPeak,
	}

	id, err := db.RecordRun(ctx, run)
	if err != nil {
		t.Fatalf("RecordRun() error = %v", err)
	}

	if id <= 0 {
		t.Errorf("RecordRun() returned id = %d, want > 0", id)
	}

	// Clean up: delete the test run
	_, err = db.Pool.Exec(ctx, "DELETE FROM benchmark_runs WHERE id = $1", id)
	if err != nil {
		t.Logf("Warning: failed to clean up test run: %v", err)
	}
}

func TestRecordRun_DefaultClient(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run with empty client should default to "go"
	run := &BenchmarkRun{
		Scenario:    "balance",
		Protocol:    "rest",
		Client:      "", // Empty, should default to "go"
		Concurrency: 5,
		DurationSec: 10,
	}

	id, err := db.RecordRun(ctx, run)
	if err != nil {
		t.Fatalf("RecordRun() error = %v", err)
	}

	// Verify client was set to default
	var client string
	err = db.Pool.QueryRow(ctx, "SELECT client FROM benchmark_runs WHERE id = $1", id).Scan(&client)
	if err != nil {
		t.Fatalf("Failed to query client: %v", err)
	}
	if client != "go" {
		t.Errorf("Client = %q, want %q", client, "go")
	}

	// Clean up
	_, _ = db.Pool.Exec(ctx, "DELETE FROM benchmark_runs WHERE id = $1", id)
}

func TestRecordSample(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First create a run
	run := &BenchmarkRun{
		Scenario:    "balance",
		Protocol:    "grpc",
		Client:      "go-test",
		Concurrency: 1,
		DurationSec: 5,
	}
	runID, err := db.RecordRun(ctx, run)
	if err != nil {
		t.Fatalf("RecordRun() error = %v", err)
	}

	// Record a sample
	sample := &BenchmarkSample{
		RunID:     runID,
		LatencyMs: 1.5,
		Success:   true,
		Timestamp: time.Now(),
	}

	err = db.RecordSample(ctx, sample)
	if err != nil {
		t.Fatalf("RecordSample() error = %v", err)
	}

	// Verify sample was recorded
	var count int
	err = db.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM benchmark_samples WHERE run_id = $1",
		runID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count samples: %v", err)
	}
	if count != 1 {
		t.Errorf("Sample count = %d, want 1", count)
	}

	// Clean up (cascade delete will remove samples)
	_, _ = db.Pool.Exec(ctx, "DELETE FROM benchmark_runs WHERE id = $1", runID)
}

func TestRecordSamples_Bulk(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a run
	run := &BenchmarkRun{
		Scenario:    "balance",
		Protocol:    "grpc",
		Client:      "go-test",
		Concurrency: 1,
		DurationSec: 5,
	}
	runID, err := db.RecordRun(ctx, run)
	if err != nil {
		t.Fatalf("RecordRun() error = %v", err)
	}

	// Create multiple samples
	samples := make([]*BenchmarkSample, 100)
	now := time.Now()
	for i := range samples {
		samples[i] = &BenchmarkSample{
			RunID:     runID,
			LatencyMs: float64(i) + 0.5,
			Success:   i%10 != 0, // 10% errors
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
		}
	}

	err = db.RecordSamples(ctx, samples)
	if err != nil {
		t.Fatalf("RecordSamples() error = %v", err)
	}

	// Verify all samples were recorded
	var count int
	err = db.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM benchmark_samples WHERE run_id = $1",
		runID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count samples: %v", err)
	}
	if count != 100 {
		t.Errorf("Sample count = %d, want 100", count)
	}

	// Clean up
	_, _ = db.Pool.Exec(ctx, "DELETE FROM benchmark_runs WHERE id = $1", runID)
}

func TestRecordSamples_Empty(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Recording empty samples should succeed (no-op)
	err := db.RecordSamples(ctx, []*BenchmarkSample{})
	if err != nil {
		t.Errorf("RecordSamples([]) error = %v, want nil", err)
	}
}

func TestGetStats(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a run with samples
	run := &BenchmarkRun{
		Scenario:    "balance",
		Protocol:    "grpc",
		Client:      "go-test",
		Concurrency: 1,
		DurationSec: 5,
	}
	runID, err := db.RecordRun(ctx, run)
	if err != nil {
		t.Fatalf("RecordRun() error = %v", err)
	}

	// Add samples with known latencies
	samples := make([]*BenchmarkSample, 100)
	now := time.Now()
	for i := range samples {
		samples[i] = &BenchmarkSample{
			RunID:     runID,
			LatencyMs: float64(i + 1), // 1ms to 100ms
			Success:   true,
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
		}
	}
	if err := db.RecordSamples(ctx, samples); err != nil {
		t.Fatalf("RecordSamples() error = %v", err)
	}

	// Get stats
	stats, err := db.GetStats(ctx, runID)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}

	if stats.RunID != runID {
		t.Errorf("RunID = %d, want %d", stats.RunID, runID)
	}
	if stats.TotalSamples != 100 {
		t.Errorf("TotalSamples = %d, want 100", stats.TotalSamples)
	}
	if stats.Successful != 100 {
		t.Errorf("Successful = %d, want 100", stats.Successful)
	}
	if stats.MinLatency != 1.0 {
		t.Errorf("MinLatency = %f, want 1.0", stats.MinLatency)
	}
	if stats.MaxLatency != 100.0 {
		t.Errorf("MaxLatency = %f, want 100.0", stats.MaxLatency)
	}
	// P50 should be around 50
	if stats.P50Latency < 45 || stats.P50Latency > 55 {
		t.Errorf("P50Latency = %f, want ~50", stats.P50Latency)
	}

	// Clean up
	_, _ = db.Pool.Exec(ctx, "DELETE FROM benchmark_runs WHERE id = $1", runID)
}
