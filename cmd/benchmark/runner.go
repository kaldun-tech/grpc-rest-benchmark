package main

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// Sample represents a single benchmark measurement.
type Sample struct {
	Latency   time.Duration
	Success   bool
	Error     error
	Timestamp time.Time
}

// Runner manages benchmark load generation.
type Runner struct {
	client       BenchmarkClient
	accountIDs   []string
	concurrency  int
	rate         int
	results      chan Sample
	mu           sync.Mutex
	rng          *rand.Rand
	timingReplay *TimingReplay // Optional timing replay for realistic workloads
}

// NewRunner creates a new benchmark runner.
func NewRunner(client BenchmarkClient, accountIDs []string, concurrency, rate int) *Runner {
	return &Runner{
		client:       client,
		accountIDs:   accountIDs,
		concurrency:  concurrency,
		rate:         rate,
		results:      make(chan Sample, 10000),
		rng:          rand.New(rand.NewSource(time.Now().UnixNano())),
		timingReplay: nil,
	}
}

// SetTimingReplay sets the timing replay for realistic workload pacing.
func (r *Runner) SetTimingReplay(tr *TimingReplay) {
	r.timingReplay = tr
}

// Results returns the channel for receiving benchmark samples.
func (r *Runner) Results() <-chan Sample {
	return r.results
}

// RunBalance executes the balance query benchmark.
func (r *Runner) RunBalance(ctx context.Context) {
	var wg sync.WaitGroup

	for i := 0; i < r.concurrency; i++ {
		wg.Add(1)
		go r.balanceWorker(ctx, &wg)
	}

	wg.Wait()
	close(r.results)
}

func (r *Runner) balanceWorker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Apply timing replay delay if configured
			if r.timingReplay != nil {
				delay := r.timingReplay.NextDelay()
				if delay > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(delay):
					}
				}
			}

			accountID := r.randomAccount()
			start := time.Now()
			err := r.client.GetBalance(ctx, accountID)
			latency := time.Since(start)

			select {
			case r.results <- Sample{
				Latency:   latency,
				Success:   err == nil,
				Error:     err,
				Timestamp: start,
			}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (r *Runner) randomAccount() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.accountIDs[r.rng.Intn(len(r.accountIDs))]
}

// RunStream executes the transaction streaming benchmark.
func (r *Runner) RunStream(ctx context.Context) {
	var wg sync.WaitGroup

	for i := 0; i < r.concurrency; i++ {
		wg.Add(1)
		go r.streamWorker(ctx, &wg)
	}

	wg.Wait()
	close(r.results)
}

func (r *Runner) streamWorker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	eventCh, errCh := r.client.StreamTransactions(ctx, r.rate)

	var lastEvent time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}

			var latency time.Duration
			if !lastEvent.IsZero() {
				latency = event.ReceivedAt.Sub(lastEvent)
			}
			lastEvent = event.ReceivedAt

			select {
			case r.results <- Sample{
				Latency:   latency,
				Success:   true,
				Timestamp: event.ReceivedAt,
			}:
			case <-ctx.Done():
				return
			}
		case err := <-errCh:
			if err != nil && ctx.Err() == nil {
				select {
				case r.results <- Sample{
					Success:   false,
					Error:     err,
					Timestamp: time.Now(),
				}:
				case <-ctx.Done():
				}
			}
			return
		}
	}
}
