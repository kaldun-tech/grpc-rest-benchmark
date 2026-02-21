package main

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/process"
)

// ResourceStats holds aggregated resource usage metrics.
type ResourceStats struct {
	CPUAvgPercent  float64
	MemoryAvgMB    float64
	MemoryPeakMB   float64
	SampleCount    int
	GoroutineCount int
}

// ResourceMonitor samples CPU and memory usage during benchmark execution.
type ResourceMonitor struct {
	proc     *process.Process
	interval time.Duration

	mu           sync.Mutex
	cpuSamples   []float64
	memSamples   []float64
	memPeak      float64
	sampleCount  int
	lastCPUTimes *cpu.TimesStat
	lastCPUTime  time.Time
}

// NewResourceMonitor creates a new monitor for the current process.
func NewResourceMonitor(interval time.Duration) (*ResourceMonitor, error) {
	proc, err := process.NewProcess(int32(getPid()))
	if err != nil {
		return nil, err
	}

	return &ResourceMonitor{
		proc:       proc,
		interval:   interval,
		cpuSamples: make([]float64, 0, 100),
		memSamples: make([]float64, 0, 100),
	}, nil
}

func getPid() int {
	return os.Getpid()
}

// Start begins collecting resource samples in the background.
// Returns a stop function that should be called when monitoring is complete.
func (m *ResourceMonitor) Start(ctx context.Context) func() ResourceStats {
	// Take initial CPU reading for delta calculation
	m.lastCPUTimes, _ = m.proc.TimesWithContext(ctx)
	m.lastCPUTime = time.Now()

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C:
				m.sample(ctx)
			}
		}
	}()

	return func() ResourceStats {
		close(stopCh)
		<-doneCh
		return m.Stats()
	}
}

func (m *ResourceMonitor) sample(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Sample memory
	memInfo, err := m.proc.MemoryInfoWithContext(ctx)
	if err == nil {
		memMB := float64(memInfo.RSS) / (1024 * 1024)
		m.memSamples = append(m.memSamples, memMB)
		if memMB > m.memPeak {
			m.memPeak = memMB
		}
	}

	// Sample CPU (calculate delta since last sample)
	cpuTimes, err := m.proc.TimesWithContext(ctx)
	if err == nil && m.lastCPUTimes != nil {
		now := time.Now()
		elapsed := now.Sub(m.lastCPUTime).Seconds()
		if elapsed > 0 {
			// Total CPU time used since last sample
			cpuDelta := (cpuTimes.User + cpuTimes.System) -
				(m.lastCPUTimes.User + m.lastCPUTimes.System)
			// Percentage of elapsed wall-clock time
			cpuPercent := (cpuDelta / elapsed) * 100
			m.cpuSamples = append(m.cpuSamples, cpuPercent)
		}
		m.lastCPUTimes = cpuTimes
		m.lastCPUTime = now
	}

	m.sampleCount++
}

// Stats returns aggregated resource statistics.
func (m *ResourceMonitor) Stats() ResourceStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats := ResourceStats{
		SampleCount:    m.sampleCount,
		MemoryPeakMB:   m.memPeak,
		GoroutineCount: runtime.NumGoroutine(),
	}

	if len(m.cpuSamples) > 0 {
		var total float64
		for _, v := range m.cpuSamples {
			total += v
		}
		stats.CPUAvgPercent = total / float64(len(m.cpuSamples))
	}

	if len(m.memSamples) > 0 {
		var total float64
		for _, v := range m.memSamples {
			total += v
		}
		stats.MemoryAvgMB = total / float64(len(m.memSamples))
	}

	return stats
}
