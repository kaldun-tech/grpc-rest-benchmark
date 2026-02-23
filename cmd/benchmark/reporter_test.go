package main

import (
	"errors"
	"testing"
	"time"
)

func TestNewResults(t *testing.T) {
	r := NewResults()
	if r == nil {
		t.Fatal("NewResults() returned nil")
	}
	if r.samples == nil {
		t.Error("samples slice is nil")
	}
	if len(r.samples) != 0 {
		t.Errorf("samples length = %d, want 0", len(r.samples))
	}
}

func TestResults_Add(t *testing.T) {
	r := NewResults()

	sample := Sample{
		Latency:   time.Millisecond,
		Success:   true,
		Timestamp: time.Now(),
	}
	r.Add(sample)

	if len(r.samples) != 1 {
		t.Errorf("samples length = %d, want 1", len(r.samples))
	}
}

func TestResults_TotalRequests(t *testing.T) {
	r := NewResults()

	for i := 0; i < 10; i++ {
		r.Add(Sample{Latency: time.Millisecond, Success: true})
	}

	if r.TotalRequests() != 10 {
		t.Errorf("TotalRequests() = %d, want 10", r.TotalRequests())
	}
}

func TestResults_SuccessfulRequests(t *testing.T) {
	r := NewResults()

	// Add 7 successful and 3 failed
	for i := 0; i < 7; i++ {
		r.Add(Sample{Latency: time.Millisecond, Success: true})
	}
	for i := 0; i < 3; i++ {
		r.Add(Sample{Latency: time.Millisecond, Success: false, Error: errors.New("test error")})
	}

	if r.SuccessfulRequests() != 7 {
		t.Errorf("SuccessfulRequests() = %d, want 7", r.SuccessfulRequests())
	}
}

func TestResults_ErrorRate(t *testing.T) {
	r := NewResults()

	// 8 successful, 2 failed = 20% error rate
	for i := 0; i < 8; i++ {
		r.Add(Sample{Latency: time.Millisecond, Success: true})
	}
	for i := 0; i < 2; i++ {
		r.Add(Sample{Latency: time.Millisecond, Success: false})
	}

	rate := r.ErrorRate()
	if rate != 20.0 {
		t.Errorf("ErrorRate() = %f, want 20.0", rate)
	}
}

func TestResults_ErrorRate_Empty(t *testing.T) {
	r := NewResults()

	rate := r.ErrorRate()
	if rate != 0 {
		t.Errorf("ErrorRate() = %f, want 0", rate)
	}
}

func TestResults_Throughput(t *testing.T) {
	r := NewResults()

	// Add 100 samples
	for i := 0; i < 100; i++ {
		r.Add(Sample{Latency: time.Millisecond, Success: true})
	}

	// Simulate 10 second duration
	r.SetStartTime(time.Now().Add(-10 * time.Second))
	r.SetEndTime(time.Now())

	throughput := r.Throughput()
	// Should be approximately 10 req/s
	if throughput < 9 || throughput > 11 {
		t.Errorf("Throughput() = %f, want ~10", throughput)
	}
}

func TestResults_Throughput_ZeroDuration(t *testing.T) {
	r := NewResults()
	r.Add(Sample{Latency: time.Millisecond, Success: true})

	now := time.Now()
	r.SetStartTime(now)
	r.SetEndTime(now)

	throughput := r.Throughput()
	if throughput != 0 {
		t.Errorf("Throughput() = %f, want 0 for zero duration", throughput)
	}
}

func TestResults_Duration(t *testing.T) {
	r := NewResults()

	start := time.Now()
	end := start.Add(5 * time.Second)
	r.SetStartTime(start)
	r.SetEndTime(end)

	duration := r.Duration()
	if duration != 5*time.Second {
		t.Errorf("Duration() = %v, want 5s", duration)
	}
}

func TestResults_Percentile(t *testing.T) {
	r := NewResults()

	// Add samples with latencies 1ms to 100ms
	for i := 1; i <= 100; i++ {
		r.Add(Sample{
			Latency: time.Duration(i) * time.Millisecond,
			Success: true,
		})
	}

	// P50 should be around 50ms
	p50 := r.Percentile(50)
	if p50 < 49*time.Millisecond || p50 > 51*time.Millisecond {
		t.Errorf("Percentile(50) = %v, want ~50ms", p50)
	}

	// P90 should be around 90ms
	p90 := r.Percentile(90)
	if p90 < 89*time.Millisecond || p90 > 91*time.Millisecond {
		t.Errorf("Percentile(90) = %v, want ~90ms", p90)
	}

	// P99 should be around 99ms
	p99 := r.Percentile(99)
	if p99 < 98*time.Millisecond || p99 > 100*time.Millisecond {
		t.Errorf("Percentile(99) = %v, want ~99ms", p99)
	}
}

func TestResults_Percentile_Empty(t *testing.T) {
	r := NewResults()

	p50 := r.Percentile(50)
	if p50 != 0 {
		t.Errorf("Percentile(50) = %v, want 0 for empty results", p50)
	}
}

func TestResults_Percentile_OnlyErrors(t *testing.T) {
	r := NewResults()

	// Add only failed samples
	for i := 0; i < 10; i++ {
		r.Add(Sample{
			Latency: time.Millisecond,
			Success: false,
		})
	}

	p50 := r.Percentile(50)
	if p50 != 0 {
		t.Errorf("Percentile(50) = %v, want 0 when all failed", p50)
	}
}

func TestResults_AvgLatency(t *testing.T) {
	r := NewResults()

	// Add samples: 10ms, 20ms, 30ms -> avg = 20ms
	r.Add(Sample{Latency: 10 * time.Millisecond, Success: true})
	r.Add(Sample{Latency: 20 * time.Millisecond, Success: true})
	r.Add(Sample{Latency: 30 * time.Millisecond, Success: true})

	avg := r.AvgLatency()
	if avg != 20*time.Millisecond {
		t.Errorf("AvgLatency() = %v, want 20ms", avg)
	}
}

func TestResults_AvgLatency_IgnoresFailures(t *testing.T) {
	r := NewResults()

	// Add successful: 10ms, 20ms, 30ms
	r.Add(Sample{Latency: 10 * time.Millisecond, Success: true})
	r.Add(Sample{Latency: 20 * time.Millisecond, Success: true})
	r.Add(Sample{Latency: 30 * time.Millisecond, Success: true})
	// Add failed: 1000ms (should be ignored)
	r.Add(Sample{Latency: 1000 * time.Millisecond, Success: false})

	avg := r.AvgLatency()
	if avg != 20*time.Millisecond {
		t.Errorf("AvgLatency() = %v, want 20ms (failures should be ignored)", avg)
	}
}

func TestResults_AvgLatency_Empty(t *testing.T) {
	r := NewResults()

	avg := r.AvgLatency()
	if avg != 0 {
		t.Errorf("AvgLatency() = %v, want 0 for empty results", avg)
	}
}

func TestResults_MinLatency(t *testing.T) {
	r := NewResults()

	r.Add(Sample{Latency: 50 * time.Millisecond, Success: true})
	r.Add(Sample{Latency: 10 * time.Millisecond, Success: true})
	r.Add(Sample{Latency: 30 * time.Millisecond, Success: true})

	min := r.MinLatency()
	if min != 10*time.Millisecond {
		t.Errorf("MinLatency() = %v, want 10ms", min)
	}
}

func TestResults_MinLatency_Empty(t *testing.T) {
	r := NewResults()

	min := r.MinLatency()
	if min != 0 {
		t.Errorf("MinLatency() = %v, want 0 for empty results", min)
	}
}

func TestResults_MaxLatency(t *testing.T) {
	r := NewResults()

	r.Add(Sample{Latency: 10 * time.Millisecond, Success: true})
	r.Add(Sample{Latency: 50 * time.Millisecond, Success: true})
	r.Add(Sample{Latency: 30 * time.Millisecond, Success: true})

	max := r.MaxLatency()
	if max != 50*time.Millisecond {
		t.Errorf("MaxLatency() = %v, want 50ms", max)
	}
}

func TestResults_MaxLatency_Empty(t *testing.T) {
	r := NewResults()

	max := r.MaxLatency()
	if max != 0 {
		t.Errorf("MaxLatency() = %v, want 0 for empty results", max)
	}
}

func TestResults_SetResourceStats(t *testing.T) {
	r := NewResults()

	stats := ResourceStats{
		CPUAvgPercent: 50.5,
		MemoryAvgMB:   100.0,
		MemoryPeakMB:  150.0,
	}
	r.SetResourceStats(stats)

	if r.resourceStats == nil {
		t.Fatal("resourceStats is nil after SetResourceStats")
	}
	if r.resourceStats.CPUAvgPercent != 50.5 {
		t.Errorf("CPUAvgPercent = %f, want 50.5", r.resourceStats.CPUAvgPercent)
	}
	if r.resourceStats.MemoryAvgMB != 100.0 {
		t.Errorf("MemoryAvgMB = %f, want 100.0", r.resourceStats.MemoryAvgMB)
	}
	if r.resourceStats.MemoryPeakMB != 150.0 {
		t.Errorf("MemoryPeakMB = %f, want 150.0", r.resourceStats.MemoryPeakMB)
	}
}

func TestResults_Collect(t *testing.T) {
	r := NewResults()

	ch := make(chan Sample, 5)

	// Add samples to channel
	for i := 0; i < 5; i++ {
		ch <- Sample{Latency: time.Duration(i) * time.Millisecond, Success: true}
	}
	close(ch)

	r.Collect(ch)

	if len(r.samples) != 5 {
		t.Errorf("samples length = %d after Collect, want 5", len(r.samples))
	}
}

func TestFormatLatency(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{500 * time.Microsecond, "500.00us"},
		{999 * time.Microsecond, "999.00us"},
		{1 * time.Millisecond, "1.00ms"},
		{1500 * time.Microsecond, "1.50ms"},
		{10 * time.Millisecond, "10.00ms"},
		{100 * time.Millisecond, "100.00ms"},
	}

	for _, tt := range tests {
		got := formatLatency(tt.input)
		if got != tt.expected {
			t.Errorf("formatLatency(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
