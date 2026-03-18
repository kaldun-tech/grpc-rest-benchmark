package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kaldun-tech/hiero-hcs-replay"
)

// TimingReplay wraps hcsreplay.Replay to provide realistic inter-arrival delays
// based on HCS timing data. This enables benchmarks to simulate realistic Hedera
// workload patterns rather than synthetic uniform traffic.
type TimingReplay struct {
	replay *hcsreplay.Replay
}

// LoadTimingData loads timing data from a JSON file.
// The JSON format is compatible with hcsreplay timing files.
func LoadTimingData(path string) (*hcsreplay.TimingData, error) {
	return hcsreplay.LoadTiming(path)
}

// FetchTimingData fetches timing data directly from an HCS topic via the
// Hedera Mirror Node REST API.
func FetchTimingData(ctx context.Context, topicID string, network string, limit int, onProgress func(int)) (*hcsreplay.TimingData, error) {
	// Convert network string to hcsreplay.Network
	var net hcsreplay.Network
	switch network {
	case "mainnet":
		net = hcsreplay.Mainnet
	case "testnet":
		net = hcsreplay.Testnet
	case "previewnet":
		net = hcsreplay.Previewnet
	default:
		return nil, fmt.Errorf("unknown network: %s (use mainnet, testnet, or previewnet)", network)
	}

	opts := hcsreplay.DefaultFetchOptions()
	opts.OnProgress = onProgress

	return hcsreplay.FetchTimingWithOptions(ctx, topicID, net, limit, opts)
}

// SaveTimingData saves timing data to a JSON file for later reuse.
func SaveTimingData(path string, data *hcsreplay.TimingData) error {
	return hcsreplay.SaveTiming(path, data)
}

// NewTimingReplay creates a new timing replay from loaded data.
// mode can be "sequential" (exact order) or "sample" (random sampling).
// speedup controls replay speed (1.0 = real-time, 10.0 = 10x faster).
func NewTimingReplay(data *hcsreplay.TimingData, mode string, speedup float64) *TimingReplay {
	var replayMode hcsreplay.ReplayMode
	switch mode {
	case "sequential":
		replayMode = hcsreplay.ModeSequential
	default:
		replayMode = hcsreplay.ModeSample
	}

	return &TimingReplay{
		replay: hcsreplay.NewReplay(data, replayMode, speedup),
	}
}

// NextDelay returns the next inter-arrival delay to use before the next operation.
// This method is thread-safe and can be called from multiple goroutines.
func (t *TimingReplay) NextDelay() time.Duration {
	return t.replay.NextDelay()
}

// EffectiveRate returns the effective message rate after applying speedup.
func (t *TimingReplay) EffectiveRate() float64 {
	return t.replay.EffectiveRate()
}

// Data returns the underlying timing data.
func (t *TimingReplay) Data() *hcsreplay.TimingData {
	return t.replay.Data()
}

// PrintSummary prints timing data summary to stdout.
func (t *TimingReplay) PrintSummary() {
	data := t.replay.Data()
	fmt.Printf("Timing replay loaded:\n")
	fmt.Printf("  Source: %s topic %s\n", data.Network, data.TopicID)
	fmt.Printf("  Messages: %d over %.1fs (%.2f msg/s)\n",
		data.MessageCount, data.TimeSpanSeconds, data.AvgRatePerSecond)
	fmt.Printf("  Inter-arrival: p50=%.1fms, p99=%.1fms\n",
		data.Stats.P50Ms, data.Stats.P99Ms)
	fmt.Printf("  Mode: %s, speedup: %.1fx\n", t.replay.Mode(), t.replay.Speedup())
	fmt.Printf("  Effective rate: ~%.2f req/s per worker\n", t.EffectiveRate())
}

// GenerateSyntheticTiming creates synthetic timing data for testing.
// Distribution follows a log-normal pattern typical of real traffic.
func GenerateSyntheticTiming(count int, avgMs, stddevMs float64) *hcsreplay.TimingData {
	return hcsreplay.GenerateSynthetic(count, avgMs, stddevMs)
}
