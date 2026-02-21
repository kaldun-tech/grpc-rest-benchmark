# gRPC vs REST Benchmark — Project Plan

## What This Is

A benchmarking tool comparing gRPC and REST performance across two scenarios relevant to financial/blockchain infrastructure. Go servers are complete. Phase 2 adds multi-language clients (Python, Rust) against the same servers to measure SDK abstraction overhead vs raw transport.

---

## Current State

**Phase 1 is complete.** Both servers are running, the benchmark CLI works, and results are persisted to PostgreSQL.

Phase 1 results:
- High concurrency (50+): gRPC wins ~17-18% throughput, 15-22% better p99 latency
- Streaming: performance parity between SSE and gRPC streaming
- Both protocols hit DB connection pool saturation around 10K req/s

**Phase 2a is complete.** Python gRPC client implemented. Migration 002 applied (adds `client` column to track language/client type).

**Phase 2b is complete.** Python Hedera SDK client implemented and tested against Hedera testnet.

Phase 2b results (run_id: 43):
- 53 req/s throughput (vs ~3000+ req/s for local gRPC)
- 93ms p50 latency (network round-trip to testnet)
- 33% error rate from Hedera rate limiting at concurrency=5

**Phase 2c is complete.** All clients (Go, Python gRPC, Python SDK) now track CPU/memory usage during benchmarks.

---

## Architecture

```
┌──────────────────────────────────────┐
│         Benchmark Controller         │
│      cmd/benchmark/main.go           │
└────────────┬─────────────────────────┘
             │
    ┌────────┴────────┐
    │                 │
┌───▼────┐       ┌────▼───┐
│  REST  │       │  gRPC  │
│ :8080  │       │ :50051 │
└───┬────┘       └────┬───┘
    │                 │
    └────────┬────────┘
             │
      ┌──────▼──────┐
      │  PostgreSQL │
      └─────────────┘

Phase 2 clients:
  ✅ Python grpcio     → local gRPC server
  ✅ Python Hedera SDK → Hedera testnet
  ⬚ Rust/tonic        → planned
```

---

## File Structure

```
grpc-rest-benchmark/
├── cmd/
│   ├── grpc-server/main.go       # gRPC server, port 50051
│   ├── rest-server/main.go       # REST server, port 8080
│   └── benchmark/main.go         # CLI benchmark runner
├── pkg/
│   ├── db/
│   │   ├── db.go                 # Connection pool setup
│   │   ├── accounts.go           # GetBalance, GetBalances, GetRandomAccountID
│   │   ├── transactions.go       # StreamTransactions (channel-based)
│   │   └── benchmark.go          # RecordRun, RecordSample, GetStats
│   └── protos/
│       ├── benchmark.proto
│       ├── benchmark.pb.go       # Generated
│       └── benchmark_grpc.pb.go  # Generated
├── clients/
│   └── python/
│       ├── grpc_client.py        # Python gRPC benchmark client
│       ├── sdk_client.py         # Python Hedera SDK benchmark client
│       ├── requirements.txt      # grpcio, psycopg, hiero-sdk-python
│       └── generate_proto.sh     # Proto stub generation
├── migrations/
│   ├── 001_init.sql              # Schema: accounts, transactions, benchmark tables
│   └── 002_add_client_column.sql # Adds client column for multi-language tracking
├── scripts/seed_data.sql         # 10K accounts, 100K transactions
├── docker-compose.yml            # PostgreSQL 16
└── Makefile                      # proto, seed, python-benchmark, etc.
```

---

## Scenarios

### Scenario 1: Token Balance Queries ✅ Complete
- `GET /api/v1/accounts/{id}/balance` (REST, ~100 bytes JSON)
- `BalanceService.GetBalance()` (gRPC, ~50 bytes protobuf)
- Workload: random account selection, no caching, concurrency levels 1/10/50/100/200
- Metrics: p50/p90/p99 latency, throughput (req/s), error rate

### Scenario 2: Transaction Stream Processing ✅ Complete
- `GET /api/v1/transactions/stream` (REST, SSE with `text/event-stream`)
- `TransactionService.StreamTransactions()` (gRPC, server streaming RPC)
- Workload: 10/100/500/1000 tx/sec at 1-10 clients
- Metrics: event delivery latency, throughput, missed events

---

## Benchmark CLI

```bash
./benchmark --scenario=balance --protocol=grpc --concurrency=50 --duration=60s
./benchmark --scenario=stream --protocol=rest --rate=100 --duration=60s
```

---

## Phase 2: Multi-Language Clients 🔄 IN PROGRESS

Goal: measure SDK abstraction overhead vs raw transport across languages. All clients hit the existing Go servers — no server changes needed.

### 2a. Python raw gRPC client ✅ Complete
- **Location:** `clients/python/grpc_client.py`
- Use `grpcio` + generated proto stubs
- Implements both scenarios (balance queries + streaming)
- Results tagged with `client=python-grpc` in PostgreSQL
- Run: `make python-benchmark ARGS="--scenario=balance --duration=30"`

### 2b. Python Hedera SDK client ✅ Complete (awaiting first test)
- **Location:** `clients/python/sdk_client.py`
- SDK repo: https://github.com/hiero-ledger/hiero-sdk-python
- Uses `hiero-sdk-python>=0.2.0` with `CryptoGetAccountBalanceQuery`
- Implements balance query scenario only (SDK doesn't expose raw streaming cleanly)
- Three-way comparison: raw gRPC vs raw REST vs SDK
- Run: `make python-sdk-benchmark ARGS="--duration=30"`
- Credentials: `.env` file with `HEDERA_OPERATOR_ID` and `HEDERA_OPERATOR_KEY` (get from https://portal.hedera.com/)
- Note: Keep concurrency low (default 5) to avoid Hedera rate limits

### 2c. Resource profiling ✅ Complete
- ✅ Go benchmark: uses `gopsutil` for CPU/memory tracking (samples every 100ms)
- ✅ Migration 003 applied: adds `cpu_usage_avg`, `memory_mb_avg`, `memory_mb_peak` columns
- ✅ Python clients: use `psutil` via shared `resources.py` module
- ⬚ Rust client: will use appropriate crate when implemented

### 2d. Realistic workload replay
- HCS API docs: https://docs.hedera.com/hedera/sdks-and-apis/hedera-consensus-service-api
- Replace synthetic uniform-random seed data with replayed HCS topic timing patterns
- **Location:** `scripts/replay_seed.go` or `scripts/replay_seed.py`
- Source: pull timing distribution from a public HCS topic, replay at 1x speed

### 2e. Rust client using `tonic`
- **Location:** `clients/rust/src/main.rs`
- Implement balance query scenario first, stretch to streaming
- Use `tonic` for gRPC, `reqwest` for REST baseline
- Same CLI flags pattern as Go benchmark runner

### 2f. Unit tests
- `pkg/db/*_test.go` — test query functions against a test DB
- `cmd/benchmark/*_test.go` — test latency measurement and result aggregation

### 2g. Connection pooling audit
- Review `pkg/db/db.go` pool config (MaxConns, MinConns, MaxConnLifetime)
- Add retry logic with exponential backoff for transient connection errors
- Verify Python and Rust clients use equivalent pooling strategies

---

## Phase 3: Dashboard & Docs 📋 PLANNED

- Single-page HTML + Chart.js dashboard reading from PostgreSQL
  - Latency distribution charts (p50/p90/p99 per protocol/language)
  - Throughput comparison bar charts
- Results API endpoint (`GET /api/v1/results?scenario=balance&run_id=...`)
- README with setup instructions and results summary
- Blog post: "When should you use gRPC? Here's the data"

---

## Quick Start

```bash
# Start DB and seed
make db-up && make seed

# Run servers (separate terminals)
make grpc-server
make rest-server

# Run a benchmark
make benchmark ARGS="--scenario=balance --protocol=grpc --concurrency=50 --duration=60s"

# Verify
curl http://localhost:8080/health
curl http://localhost:8080/api/v1/accounts/0.0.100000/balance
grpcurl -plaintext localhost:50051 list
```