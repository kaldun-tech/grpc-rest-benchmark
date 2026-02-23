package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadTimingData(t *testing.T) {
	// Create a temporary timing file
	data := TimingData{
		TopicID:          "0.0.123456",
		Network:          "testnet",
		MessageCount:     5,
		TimeSpanSeconds:  10.0,
		AvgRatePerSecond: 0.5,
		InterArrivalMs:   []float64{100, 200, 150, 300, 250},
		Stats: TimingStats{
			MinMs: 100,
			MaxMs: 300,
			AvgMs: 200,
			P50Ms: 200,
			P90Ms: 300,
			P99Ms: 300,
		},
	}

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "timing.json")

	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Failed to marshal test data: %v", err)
	}

	if err := os.WriteFile(tmpFile, jsonData, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test loading
	loaded, err := LoadTimingData(tmpFile)
	if err != nil {
		t.Fatalf("LoadTimingData() error = %v", err)
	}

	if loaded.TopicID != data.TopicID {
		t.Errorf("TopicID = %q, want %q", loaded.TopicID, data.TopicID)
	}
	if loaded.MessageCount != data.MessageCount {
		t.Errorf("MessageCount = %d, want %d", loaded.MessageCount, data.MessageCount)
	}
	if len(loaded.InterArrivalMs) != len(data.InterArrivalMs) {
		t.Errorf("InterArrivalMs length = %d, want %d", len(loaded.InterArrivalMs), len(data.InterArrivalMs))
	}
}

func TestLoadTimingData_NotFound(t *testing.T) {
	_, err := LoadTimingData("/nonexistent/path/timing.json")
	if err == nil {
		t.Error("LoadTimingData() expected error for non-existent file, got nil")
	}
}

func TestLoadTimingData_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "invalid.json")

	if err := os.WriteFile(tmpFile, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, err := LoadTimingData(tmpFile)
	if err == nil {
		t.Error("LoadTimingData() expected error for invalid JSON, got nil")
	}
}

func TestLoadTimingData_EmptyInterArrivals(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "empty.json")

	data := TimingData{
		TopicID:        "0.0.123",
		InterArrivalMs: []float64{}, // Empty
	}
	jsonData, _ := json.Marshal(data)
	if err := os.WriteFile(tmpFile, jsonData, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, err := LoadTimingData(tmpFile)
	if err == nil {
		t.Error("LoadTimingData() expected error for empty inter-arrivals, got nil")
	}
}

func TestNewTimingReplay(t *testing.T) {
	data := &TimingData{
		InterArrivalMs: []float64{100, 200, 300},
	}

	tr := NewTimingReplay(data, "sample", 1.0)
	if tr == nil {
		t.Fatal("NewTimingReplay() returned nil")
	}
	if tr.mode != "sample" {
		t.Errorf("mode = %q, want %q", tr.mode, "sample")
	}
	if tr.speedup != 1.0 {
		t.Errorf("speedup = %f, want 1.0", tr.speedup)
	}
}

func TestNewTimingReplay_InvalidSpeedup(t *testing.T) {
	data := &TimingData{
		InterArrivalMs: []float64{100, 200, 300},
	}

	// Zero speedup should default to 1.0
	tr := NewTimingReplay(data, "sample", 0)
	if tr.speedup != 1.0 {
		t.Errorf("speedup = %f, want 1.0 (default)", tr.speedup)
	}

	// Negative speedup should default to 1.0
	tr = NewTimingReplay(data, "sample", -5)
	if tr.speedup != 1.0 {
		t.Errorf("speedup = %f, want 1.0 (default)", tr.speedup)
	}
}

func TestTimingReplay_NextDelay_Sequential(t *testing.T) {
	data := &TimingData{
		InterArrivalMs: []float64{100, 200, 300},
	}

	tr := NewTimingReplay(data, "sequential", 1.0)

	// Sequential mode should return values in order
	expected := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		100 * time.Millisecond, // Wraps around
	}

	for i, want := range expected {
		got := tr.NextDelay()
		if got != want {
			t.Errorf("NextDelay() #%d = %v, want %v", i, got, want)
		}
	}
}

func TestTimingReplay_NextDelay_Sample(t *testing.T) {
	data := &TimingData{
		InterArrivalMs: []float64{100, 200, 300},
	}

	tr := NewTimingReplay(data, "sample", 1.0)

	// Sample mode should return values from the distribution
	validDelays := map[time.Duration]bool{
		100 * time.Millisecond: true,
		200 * time.Millisecond: true,
		300 * time.Millisecond: true,
	}

	for i := 0; i < 10; i++ {
		got := tr.NextDelay()
		if !validDelays[got] {
			t.Errorf("NextDelay() = %v, not in valid set", got)
		}
	}
}

func TestTimingReplay_NextDelay_Speedup(t *testing.T) {
	data := &TimingData{
		InterArrivalMs: []float64{100, 200, 300},
	}

	// 2x speedup means delays should be halved
	tr := NewTimingReplay(data, "sequential", 2.0)

	expected := []time.Duration{
		50 * time.Millisecond,  // 100 / 2
		100 * time.Millisecond, // 200 / 2
		150 * time.Millisecond, // 300 / 2
	}

	for i, want := range expected {
		got := tr.NextDelay()
		if got != want {
			t.Errorf("NextDelay() #%d = %v, want %v", i, got, want)
		}
	}
}

func TestGenerateSyntheticTiming(t *testing.T) {
	data := GenerateSyntheticTiming(100, 50.0, 20.0)

	if data == nil {
		t.Fatal("GenerateSyntheticTiming() returned nil")
	}
	if data.TopicID != "synthetic" {
		t.Errorf("TopicID = %q, want %q", data.TopicID, "synthetic")
	}
	if data.Network != "generated" {
		t.Errorf("Network = %q, want %q", data.Network, "generated")
	}
	if data.MessageCount != 100 {
		t.Errorf("MessageCount = %d, want 100", data.MessageCount)
	}
	if len(data.InterArrivalMs) != 100 {
		t.Errorf("InterArrivalMs length = %d, want 100", len(data.InterArrivalMs))
	}

	// Verify all values are positive
	for i, v := range data.InterArrivalMs {
		if v < 1 {
			t.Errorf("InterArrivalMs[%d] = %f, want >= 1", i, v)
		}
	}

	// Stats should be populated
	if data.Stats.MinMs <= 0 {
		t.Error("Stats.MinMs should be > 0")
	}
	if data.Stats.MaxMs < data.Stats.MinMs {
		t.Error("Stats.MaxMs should be >= MinMs")
	}
	if data.Stats.AvgMs <= 0 {
		t.Error("Stats.AvgMs should be > 0")
	}
}

func TestHelperFunctions(t *testing.T) {
	// Test average
	vals := []float64{10, 20, 30, 40, 50}
	avg := average(vals)
	if avg != 30.0 {
		t.Errorf("average() = %f, want 30.0", avg)
	}

	// Test average empty
	if avg := average([]float64{}); avg != 0 {
		t.Errorf("average([]) = %f, want 0", avg)
	}

	// Test sum
	s := sum(vals)
	if s != 150.0 {
		t.Errorf("sum() = %f, want 150.0", s)
	}

	// Test percentile
	sorted := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	p50 := percentile(sorted, 0.50)
	if p50 != 6.0 { // Index 5 at 50%
		t.Errorf("percentile(50) = %f, want 6.0", p50)
	}

	p90 := percentile(sorted, 0.90)
	if p90 != 10.0 { // Index 9 at 90%
		t.Errorf("percentile(90) = %f, want 10.0", p90)
	}

	// Test percentile empty
	if p := percentile([]float64{}, 0.50); p != 0 {
		t.Errorf("percentile([]) = %f, want 0", p)
	}
}
