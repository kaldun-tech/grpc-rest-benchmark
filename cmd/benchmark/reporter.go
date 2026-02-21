package main

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/kaldun-tech/grpc-rest-benchmark/pkg/db"
)

// Results collects and analyzes benchmark samples.
type Results struct {
	samples       []Sample
	startTime     time.Time
	endTime       time.Time
	resourceStats *ResourceStats
}

// NewResults creates a new Results collector.
func NewResults() *Results {
	return &Results{
		samples: make([]Sample, 0, 10000),
	}
}

// SetStartTime records when the benchmark started.
func (r *Results) SetStartTime(t time.Time) {
	r.startTime = t
}

// SetEndTime records when the benchmark ended.
func (r *Results) SetEndTime(t time.Time) {
	r.endTime = t
}

// SetResourceStats records resource usage metrics.
func (r *Results) SetResourceStats(stats ResourceStats) {
	r.resourceStats = &stats
}

// Add adds a sample to the results.
func (r *Results) Add(s Sample) {
	r.samples = append(r.samples, s)
}

// Collect reads all samples from a channel into results.
func (r *Results) Collect(ch <-chan Sample) {
	for sample := range ch {
		r.Add(sample)
	}
}

// TotalRequests returns the total number of requests.
func (r *Results) TotalRequests() int {
	return len(r.samples)
}

// SuccessfulRequests returns the count of successful requests.
func (r *Results) SuccessfulRequests() int {
	count := 0
	for _, s := range r.samples {
		if s.Success {
			count++
		}
	}
	return count
}

// ErrorRate returns the percentage of failed requests.
func (r *Results) ErrorRate() float64 {
	if len(r.samples) == 0 {
		return 0
	}
	errors := len(r.samples) - r.SuccessfulRequests()
	return float64(errors) / float64(len(r.samples)) * 100
}

// Throughput returns requests per second.
func (r *Results) Throughput() float64 {
	duration := r.endTime.Sub(r.startTime).Seconds()
	if duration == 0 {
		return 0
	}
	return float64(len(r.samples)) / duration
}

// Duration returns the benchmark duration.
func (r *Results) Duration() time.Duration {
	return r.endTime.Sub(r.startTime)
}

// Percentile returns the latency at the given percentile (0-100).
func (r *Results) Percentile(p float64) time.Duration {
	successful := r.successfulLatencies()
	if len(successful) == 0 {
		return 0
	}

	sort.Slice(successful, func(i, j int) bool {
		return successful[i] < successful[j]
	})

	idx := int(float64(len(successful)-1) * p / 100)
	return successful[idx]
}

// AvgLatency returns the average latency of successful requests.
func (r *Results) AvgLatency() time.Duration {
	successful := r.successfulLatencies()
	if len(successful) == 0 {
		return 0
	}

	var total time.Duration
	for _, l := range successful {
		total += l
	}
	return total / time.Duration(len(successful))
}

// MinLatency returns the minimum latency.
func (r *Results) MinLatency() time.Duration {
	successful := r.successfulLatencies()
	if len(successful) == 0 {
		return 0
	}

	min := successful[0]
	for _, l := range successful[1:] {
		if l < min {
			min = l
		}
	}
	return min
}

// MaxLatency returns the maximum latency.
func (r *Results) MaxLatency() time.Duration {
	successful := r.successfulLatencies()
	if len(successful) == 0 {
		return 0
	}

	max := successful[0]
	for _, l := range successful[1:] {
		if l > max {
			max = l
		}
	}
	return max
}

func (r *Results) successfulLatencies() []time.Duration {
	latencies := make([]time.Duration, 0, len(r.samples))
	for _, s := range r.samples {
		if s.Success && s.Latency > 0 {
			latencies = append(latencies, s.Latency)
		}
	}
	return latencies
}

// PrintSummary prints a formatted summary to stdout.
func (r *Results) PrintSummary(scenario, protocol string, concurrency int) {
	fmt.Printf("\nBenchmark: %s / %s\n", scenario, protocol)
	fmt.Printf("Duration: %s | Concurrency: %d\n", r.Duration().Round(time.Second), concurrency)
	fmt.Println(("---------------------------------"))
	fmt.Printf("Requests:    %d\n", r.TotalRequests())
	fmt.Printf("Throughput:  %.2f req/s\n", r.Throughput())
	fmt.Println("Latency:")
	fmt.Printf("  p50:  %s\n", formatLatency(r.Percentile(50)))
	fmt.Printf("  p90:  %s\n", formatLatency(r.Percentile(90)))
	fmt.Printf("  p99:  %s\n", formatLatency(r.Percentile(99)))
	fmt.Printf("  avg:  %s\n", formatLatency(r.AvgLatency()))
	fmt.Printf("  min:  %s\n", formatLatency(r.MinLatency()))
	fmt.Printf("  max:  %s\n", formatLatency(r.MaxLatency()))
	fmt.Printf("Errors:      %d (%.2f%%)\n", r.TotalRequests()-r.SuccessfulRequests(), r.ErrorRate())

	if r.resourceStats != nil {
		fmt.Println("Resources:")
		fmt.Printf("  CPU avg:   %.1f%%\n", r.resourceStats.CPUAvgPercent)
		fmt.Printf("  Mem avg:   %.1f MB\n", r.resourceStats.MemoryAvgMB)
		fmt.Printf("  Mem peak:  %.1f MB\n", r.resourceStats.MemoryPeakMB)
	}
	fmt.Println()
}

func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fus", float64(d.Microseconds()))
	}
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
}

// StoreResults saves benchmark results to the database.
func (r *Results) StoreResults(ctx context.Context, database *db.DB, scenario, protocol string, concurrency int, rateLimit *int) error {
	// Create benchmark run record
	run := &db.BenchmarkRun{
		Scenario:    scenario,
		Protocol:    protocol,
		Concurrency: concurrency,
		DurationSec: int(r.Duration().Seconds()),
		RateLimit:   rateLimit,
	}

	// Add resource metrics if available
	if r.resourceStats != nil {
		run.CPUUsageAvg = &r.resourceStats.CPUAvgPercent
		run.MemoryMBAvg = &r.resourceStats.MemoryAvgMB
		run.MemoryMBPeak = &r.resourceStats.MemoryPeakMB
	}

	runID, err := database.RecordRun(ctx, run)
	if err != nil {
		return fmt.Errorf("failed to record run: %w", err)
	}

	// Convert samples for batch insert
	dbSamples := make([]*db.BenchmarkSample, 0, len(r.samples))
	for _, s := range r.samples {
		sample := &db.BenchmarkSample{
			RunID:     runID,
			LatencyMs: float64(s.Latency.Microseconds()) / 1000.0,
			Success:   s.Success,
			Timestamp: s.Timestamp,
		}
		if s.Error != nil {
			errStr := s.Error.Error()
			sample.ErrorType = &errStr
		}
		dbSamples = append(dbSamples, sample)
	}

	// Batch insert samples
	if err := database.RecordSamples(ctx, dbSamples); err != nil {
		return fmt.Errorf("failed to record samples: %w", err)
	}

	fmt.Printf("Results saved to database (run_id: %d)\n", runID)

	// Retrieve and print stats from the view
	stats, err := database.GetStats(ctx, runID)
	if err != nil {
		fmt.Printf("Warning: could not retrieve stats from view: %v\n", err)
		return nil
	}

	fmt.Printf("\nDatabase stats (from benchmark_stats view):\n")
	fmt.Printf("  p50: %.2fms, p90: %.2fms, p99: %.2fms\n",
		stats.P50Latency, stats.P90Latency, stats.P99Latency)

	return nil
}
