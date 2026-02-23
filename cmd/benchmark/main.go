package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kaldun-tech/grpc-rest-benchmark/pkg/db"
)

func main() {
	// CLI flags
	scenario := flag.String("scenario", "balance", "Benchmark scenario: balance | stream")
	protocol := flag.String("protocol", "grpc", "Protocol to test: grpc | rest")
	concurrency := flag.Int("concurrency", 10, "Number of parallel workers")
	duration := flag.Duration("duration", 30*time.Second, "Test duration (e.g., 30s, 1m)")
	rate := flag.Int("rate", 0, "Events per second for streaming (0 = unlimited)")
	grpcAddr := flag.String("grpc-addr", "localhost:50051", "gRPC server address")
	restAddr := flag.String("rest-addr", "http://localhost:8080", "REST server address")

	// Timing replay flags (Phase 2d)
	replayTiming := flag.String("replay-timing", "", "Path to HCS timing JSON file for realistic workload replay")
	replayMode := flag.String("replay-mode", "sample", "Replay mode: sequential | sample")
	replaySpeedup := flag.Float64("replay-speedup", 1.0, "Speedup factor for replay (1.0 = real-time, 10.0 = 10x faster)")

	// Database flags
	dbHost := flag.String("db-host", "localhost", "PostgreSQL host")
	dbPort := flag.Int("db-port", 5432, "PostgreSQL port")
	dbUser := flag.String("db-user", "benchmark", "PostgreSQL user")
	dbPass := flag.String("db-pass", "benchmark_pass", "PostgreSQL password")
	dbName := flag.String("db-name", "grpc_benchmark", "PostgreSQL database")

	flag.Parse()

	// Validate inputs
	if *scenario != "balance" && *scenario != "stream" {
		log.Fatalf("Invalid scenario: %s (must be 'balance' or 'stream')", *scenario)
	}
	if *protocol != "grpc" && *protocol != "rest" {
		log.Fatalf("Invalid protocol: %s (must be 'grpc' or 'rest')", *protocol)
	}
	if *concurrency < 1 {
		log.Fatalf("Concurrency must be at least 1")
	}
	if *duration < time.Second {
		log.Fatalf("Duration must be at least 1 second")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Received interrupt signal, stopping benchmark...")
		cancel()
	}()

	// Connect to database
	dbCfg := db.Config{
		Host:     *dbHost,
		Port:     *dbPort,
		User:     *dbUser,
		Password: *dbPass,
		Database: *dbName,
	}

	database, err := db.New(ctx, dbCfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Printf("Connected to database %s@%s:%d", dbCfg.Database, dbCfg.Host, dbCfg.Port)

	// Pre-fetch account IDs for balance scenario
	var accountIDs []string
	if *scenario == "balance" {
		log.Println("Loading account IDs from database...")
		accountIDs, err = database.GetAllAccountIDs(ctx)
		if err != nil {
			log.Fatalf("Failed to load account IDs: %v", err)
		}
		if len(accountIDs) == 0 {
			log.Fatal("No accounts found in database. Run 'make seed' first.")
		}
		log.Printf("Loaded %d account IDs", len(accountIDs))
	}

	// Create client based on protocol
	var client BenchmarkClient
	switch *protocol {
	case "grpc":
		client, err = NewGRPCClient(*grpcAddr)
		if err != nil {
			log.Fatalf("Failed to create gRPC client: %v", err)
		}
		log.Printf("Connected to gRPC server at %s", *grpcAddr)
	case "rest":
		client, err = NewHTTPClient(*restAddr)
		if err != nil {
			log.Fatalf("Failed to create HTTP client: %v", err)
		}
		log.Printf("Connected to REST server at %s", *restAddr)
	}
	defer client.Close()

	// Create runner
	runner := NewRunner(client, accountIDs, *concurrency, *rate)

	// Load timing replay if specified
	if *replayTiming != "" {
		timingData, err := LoadTimingData(*replayTiming)
		if err != nil {
			log.Fatalf("Failed to load timing data: %v", err)
		}
		tr := NewTimingReplay(timingData, *replayMode, *replaySpeedup)
		runner.SetTimingReplay(tr)
		tr.PrintSummary()
		fmt.Println()
	}

	// Setup results collector
	results := NewResults()

	// Setup resource monitor
	resourceMonitor, err := NewResourceMonitor(100 * time.Millisecond)
	if err != nil {
		log.Printf("Warning: could not initialize resource monitor: %v", err)
	}

	// Create context with timeout for benchmark duration
	benchCtx, benchCancel := context.WithTimeout(ctx, *duration)
	defer benchCancel()

	// Run benchmark
	fmt.Printf("\nStarting %s benchmark (%s protocol)\n", *scenario, *protocol)
	fmt.Printf("Concurrency: %d | Duration: %s", *concurrency, *duration)
	if *scenario == "stream" && *rate > 0 {
		fmt.Printf(" | Rate limit: %d events/s", *rate)
	}
	if *replayTiming != "" {
		fmt.Printf(" | Replay: %s (%.1fx)", *replayMode, *replaySpeedup)
	}
	fmt.Println()

	// Start resource monitoring
	var stopResourceMonitor func() ResourceStats
	if resourceMonitor != nil {
		stopResourceMonitor = resourceMonitor.Start(benchCtx)
	}

	results.SetStartTime(time.Now())

	// Start results collector in background
	done := make(chan struct{})
	go func() {
		results.Collect(runner.Results())
		close(done)
	}()

	// Run the benchmark
	switch *scenario {
	case "balance":
		runner.RunBalance(benchCtx)
	case "stream":
		runner.RunStream(benchCtx)
	}

	// Wait for collector to finish
	<-done

	results.SetEndTime(time.Now())

	// Stop resource monitoring and record stats
	if stopResourceMonitor != nil {
		resourceStats := stopResourceMonitor()
		results.SetResourceStats(resourceStats)
	}

	// Print summary
	results.PrintSummary(*scenario, *protocol, *concurrency)

	// Store results in database
	var rateLimit *int
	if *scenario == "stream" && *rate > 0 {
		rateLimit = rate
	}

	if err := results.StoreResults(ctx, database, *scenario, *protocol, *concurrency, rateLimit); err != nil {
		log.Printf("Warning: failed to store results: %v", err)
	}
}
