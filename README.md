# gRPC vs REST Performance Benchmark

Performance comparison of gRPC and REST protocols for Hedera-style financial infrastructure workloads (blockchain custody, exchanges, payment systems).

Built in Go with a PostgreSQL backend to isolate transport layer performance from application logic.

## Phase 1 Results

| Metric | gRPC | REST | Difference |
|--------|------|------|------------|
| Throughput (50+ concurrency) | ~17-18% higher | baseline | gRPC wins |
| p99 latency (50+ concurrency) | 15-22% lower | baseline | gRPC wins |
| Streaming throughput | ~equal | ~equal | parity |

Both protocols hit DB connection pool saturation around 10K req/s.

## Prerequisites

- **Go 1.21+** with modules enabled
- **Docker** and Docker Compose
- **Protocol Buffers** compiler (`protoc`) with Go plugins (`protoc-gen-go`, `protoc-gen-go-grpc`)
- **Python 3.12+** (optional, for Python benchmarks)
- **Rust/Cargo** (optional, for Rust benchmarks)

## Quick Start

```bash
# Generate protobuf code
make proto

# Start PostgreSQL and seed test data (10K accounts, 100K transactions)
make seed

# Start servers (in separate terminals)
make grpc-server   # starts gRPC server on :50051
make rest-server   # starts REST server on :8080

# Run a benchmark
make benchmark ARGS="--scenario=balance --protocol=grpc --concurrency=10 --duration=10s"

# View results in dashboard
open http://localhost:8080/

# Cleanup
make clean
```

## Benchmark Scenarios

### Scenario 1: Balance Queries

High-frequency unary request/response pattern simulating account balance lookups.

| Aspect | Details |
|--------|---------|
| Pattern | Unary RPC / GET request |
| Payload | ~100 bytes (account ID → balance) |
| Use case | Wallet balance checks, pre-transaction validation |
| Data | 10,000 accounts with random balances |

**gRPC:** `BalanceService.GetBalance(account_id) → BalanceResponse`

**REST:** `GET /accounts/{id}/balance → JSON`

### Scenario 2: Transaction Streaming

Server-side streaming pattern simulating real-time transaction event feeds.

| Aspect | Details |
|--------|---------|
| Pattern | Server streaming RPC / SSE |
| Payload | ~200 bytes per transaction event |
| Use case | Transaction monitoring, audit trails, real-time dashboards |
| Data | 100,000 transactions over 24-hour window |

**gRPC:** `TransactionService.StreamTransactions(since, rate_limit) → stream Transaction`

**REST:** `GET /transactions/stream?since=...` (Server-Sent Events)

## Running Benchmarks

Three benchmark clients are available: Go, Python, and Rust. All store results in PostgreSQL and can be visualized in the dashboard.

### Go Client (default)

```bash
# Balance queries
make go-benchmark ARGS="--scenario=balance --protocol=grpc --concurrency=50 --duration=30s"
make go-benchmark ARGS="--scenario=balance --protocol=rest --concurrency=50 --duration=30s"

# Transaction streaming
make go-benchmark ARGS="--scenario=stream --protocol=grpc --rate=100 --duration=30s"
make go-benchmark ARGS="--scenario=stream --protocol=rest --rate=100 --duration=30s"
```

### Python Client

The Makefile automatically creates a virtual environment at `clients/python/venv/`.

```bash
# Balance queries (gRPC only)
make python-benchmark ARGS="--scenario=balance --concurrency=10 --duration=30s"

# Transaction streaming
make python-benchmark ARGS="--scenario=stream --rate=100 --duration=30s"
```

### Rust Client

```bash
# Balance queries
make rust-benchmark ARGS="--scenario=balance --protocol=grpc --duration=30s"
make rust-benchmark ARGS="--scenario=balance --protocol=rest --duration=30s"

# Transaction streaming (gRPC only)
make rust-benchmark ARGS="--scenario=stream --protocol=grpc --rate=100 --duration=30s"
```

### HCS Timing Replay

Replay real Hedera Consensus Service timing patterns for realistic workload simulation:

```bash
# Fetch timing data from a public HCS topic
make fetch-hcs-timing TOPIC=0.0.120438 NETWORK=mainnet LIMIT=1000

# Run benchmark with timing replay (10x speedup)
make benchmark-replay TIMING=timing.json SPEEDUP=10 ARGS="--protocol=grpc --concurrency=10"
```

### Running Tests

```bash
make test              # Run all tests
make test-db           # Database integration tests only
make test-benchmark    # Benchmark unit tests only
```

## Project Structure

```
├── cmd/
│   ├── grpc-server/     # gRPC server (:50051)
│   ├── rest-server/     # REST server (:8080)
│   └── benchmark/       # CLI benchmark runner
├── pkg/
│   ├── protos/          # Protocol buffer definitions + generated code
│   └── db/              # PostgreSQL client (accounts, transactions, results)
├── migrations/          # Database schema
└── scripts/             # Seed data generation
```

## Dashboard

The REST server includes a web dashboard for visualizing benchmark results.

```bash
# Start the REST server
make rest-server

# Open dashboard in browser
open http://localhost:8080/
```

Features:
- **Latency distribution charts** — p50/p90/p99 comparison across protocols and clients
- **Throughput comparison** — req/s bar charts
- **Filter controls** — filter by scenario, protocol, client
- **Results table** — detailed view of all benchmark runs

## Results API

Query benchmark results programmatically:

```bash
# Get all results
curl http://localhost:8080/api/v1/results

# Filter by scenario
curl "http://localhost:8080/api/v1/results?scenario=balance_query"

# Filter by protocol and client
curl "http://localhost:8080/api/v1/results?protocol=grpc&client=go"

# Get specific run
curl "http://localhost:8080/api/v1/results?run_id=42"
```

Response format:
```json
{
  "results": [
    {
      "run_id": 42,
      "scenario": "balance_query",
      "protocol": "grpc",
      "client": "go",
      "concurrency": 50,
      "throughput": 3245.67,
      "p50_latency_ms": 12.5,
      "p90_latency_ms": 18.2,
      "p99_latency_ms": 25.8,
      "total_samples": 97370,
      "successful": 97370
    }
  ],
  "count": 1
}
```

## Metrics Collected

- **Latency:** p50, p90, p99, min, max, average
- **Throughput:** Requests/second, events/second
- **Error rates:** By error type
- **Resource usage:** CPU, memory (optional)

Results are stored in PostgreSQL (`benchmark_runs`, `benchmark_samples` tables) with a `benchmark_stats` view for analysis.

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `benchmark` | Database user |
| `DB_PASSWORD` | `benchmark_pass` | Database password |
| `DB_NAME` | `grpc_benchmark` | Database name |
| `GRPC_PORT` | `50051` | gRPC server port |
| `REST_PORT` | `8080` | REST server port |
