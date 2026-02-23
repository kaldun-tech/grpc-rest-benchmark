package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"time"
)

// TimingStats holds statistics about inter-arrival times.
type TimingStats struct {
	MinMs float64 `json:"min_ms"`
	MaxMs float64 `json:"max_ms"`
	AvgMs float64 `json:"avg_ms"`
	P50Ms float64 `json:"p50_ms"`
	P90Ms float64 `json:"p90_ms"`
	P99Ms float64 `json:"p99_ms"`
}

// TimingData represents the timing distribution loaded from a JSON file.
type TimingData struct {
	TopicID           string      `json:"topic_id"`
	Network           string      `json:"network"`
	MessageCount      int         `json:"message_count"`
	TimeSpanSeconds   float64     `json:"time_span_seconds"`
	AvgRatePerSecond  float64     `json:"avg_rate_per_second"`
	InterArrivalMs    []float64   `json:"inter_arrival_ms"`
	Stats             TimingStats `json:"stats"`
}

// TimingReplay provides realistic inter-arrival delays based on HCS timing data.
type TimingReplay struct {
	data      *TimingData
	rng       *rand.Rand
	index     int       // Current position for sequential replay
	mode      string    // "sequential" or "sample"
	speedup   float64   // Speedup factor (1.0 = real-time, 2.0 = 2x faster)
}

// LoadTimingData loads timing data from a JSON file.
func LoadTimingData(path string) (*TimingData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read timing file: %w", err)
	}

	var timing TimingData
	if err := json.Unmarshal(data, &timing); err != nil {
		return nil, fmt.Errorf("failed to parse timing file: %w", err)
	}

	if len(timing.InterArrivalMs) == 0 {
		return nil, fmt.Errorf("timing file has no inter-arrival data")
	}

	return &timing, nil
}

// NewTimingReplay creates a new timing replay from loaded data.
func NewTimingReplay(data *TimingData, mode string, speedup float64) *TimingReplay {
	if speedup <= 0 {
		speedup = 1.0
	}
	return &TimingReplay{
		data:    data,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
		index:   0,
		mode:    mode,
		speedup: speedup,
	}
}

// NextDelay returns the next inter-arrival delay to use.
func (t *TimingReplay) NextDelay() time.Duration {
	var delayMs float64

	switch t.mode {
	case "sequential":
		// Replay in exact sequence, wrapping around
		delayMs = t.data.InterArrivalMs[t.index]
		t.index = (t.index + 1) % len(t.data.InterArrivalMs)
	case "sample":
		// Random sample from the distribution
		delayMs = t.data.InterArrivalMs[t.rng.Intn(len(t.data.InterArrivalMs))]
	default:
		// Default to sampling
		delayMs = t.data.InterArrivalMs[t.rng.Intn(len(t.data.InterArrivalMs))]
	}

	// Apply speedup factor
	delayMs = delayMs / t.speedup

	return time.Duration(delayMs * float64(time.Millisecond))
}

// PrintSummary prints timing data summary to stdout.
func (t *TimingReplay) PrintSummary() {
	fmt.Printf("Timing replay loaded:\n")
	fmt.Printf("  Source: %s topic %s\n", t.data.Network, t.data.TopicID)
	fmt.Printf("  Messages: %d over %.1fs (%.2f msg/s)\n",
		t.data.MessageCount, t.data.TimeSpanSeconds, t.data.AvgRatePerSecond)
	fmt.Printf("  Inter-arrival: p50=%.1fms, p99=%.1fms\n",
		t.data.Stats.P50Ms, t.data.Stats.P99Ms)
	fmt.Printf("  Mode: %s, speedup: %.1fx\n", t.mode, t.speedup)

	// Effective rate after speedup
	effectiveRate := t.data.AvgRatePerSecond * t.speedup
	fmt.Printf("  Effective rate: ~%.2f req/s per worker\n", effectiveRate)
}

// GenerateSyntheticTiming creates synthetic timing data for testing.
// Distribution follows a log-normal pattern typical of real traffic.
func GenerateSyntheticTiming(count int, avgMs, stddevMs float64) *TimingData {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Generate log-normal distributed inter-arrivals
	// Log-normal is common for network traffic patterns
	interArrivals := make([]float64, count)
	for i := range interArrivals {
		// Box-Muller transform for normal distribution
		u1 := rng.Float64()
		u2 := rng.Float64()
		z := (-2 * math.Log(u1)) * math.Cos(2*math.Pi*u2)

		// Convert to log-normal
		logMean := math.Log(avgMs) - 0.5*math.Log(1+(stddevMs*stddevMs)/(avgMs*avgMs))
		logStd := math.Sqrt(math.Log(1 + (stddevMs*stddevMs)/(avgMs*avgMs)))

		value := math.Exp(logMean + logStd*z)
		if value < 1 {
			value = 1 // Minimum 1ms
		}
		interArrivals[i] = value
	}

	// Calculate stats
	sorted := make([]float64, len(interArrivals))
	copy(sorted, interArrivals)
	sort.Float64s(sorted)

	stats := TimingStats{
		MinMs: sorted[0],
		MaxMs: sorted[len(sorted)-1],
		AvgMs: average(sorted),
		P50Ms: percentile(sorted, 0.50),
		P90Ms: percentile(sorted, 0.90),
		P99Ms: percentile(sorted, 0.99),
	}

	totalMs := sum(interArrivals)
	timeSpanS := totalMs / 1000

	return &TimingData{
		TopicID:          "synthetic",
		Network:          "generated",
		MessageCount:     count,
		TimeSpanSeconds:  timeSpanS,
		AvgRatePerSecond: float64(count) / timeSpanS,
		InterArrivalMs:   interArrivals,
		Stats:            stats,
	}
}

func average(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	return sum(vals) / float64(len(vals))
}

func sum(vals []float64) float64 {
	var total float64
	for _, v := range vals {
		total += v
	}
	return total
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
