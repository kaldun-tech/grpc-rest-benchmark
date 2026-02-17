# Project Plan: gRPC vs REST Benchmark

## Progress Summary

| Phase | Status | Description |
|-------|--------|-------------|
| Infrastructure | **COMPLETE** | Schema, proto, seed data, Docker, Makefile |
| Go Dependencies | **COMPLETE** | grpc, protobuf, pgx in go.mod |
| Database Client | **COMPLETE** | pkg/db/ with all queries |
| gRPC Server | **COMPLETE** | cmd/grpc-server/ on :50051 |
| REST Server | **COMPLETE** | cmd/rest-server/ on :8080 |
| Benchmark Runner | **COMPLETE** | cmd/benchmark/ with balance & stream scenarios |

---

## Code Review (Tomorrow Morning)

**Uncommitted files to review:**

1. **`pkg/db/db.go`** - Connection pool setup, config struct
2. **`pkg/db/accounts.go`** - `GetBalance()`, `GetBalances()`, `GetRandomAccountID()`
3. **`pkg/db/transactions.go`** - `StreamTransactions()` with channel-based streaming
4. **`pkg/db/benchmark.go`** - `RecordRun()`, `RecordSample()`, `GetStats()`
5. **`cmd/grpc-server/main.go`** - gRPC server with BalanceService, TransactionService
6. **`cmd/rest-server/main.go`** - REST server with SSE streaming
7. **`Makefile`** - Added `grpc-server`, `rest-server` targets

**Review checklist:**
- [ ] DB connection handling and error wrapping
- [ ] gRPC service implementations match proto definitions
- [ ] REST endpoints match spec (`/api/v1/accounts/{id}/balance`, etc.)
- [ ] SSE streaming format correct (`event: transaction\ndata: {...}\n\n`)
- [ ] Rate limiting logic in both streaming implementations
- [ ] Graceful shutdown handling

**Quick test after review:**
```bash
# Terminal 1: Start database and seed
make db-up && make seed

# Terminal 2: Start gRPC server
make grpc-server

# Terminal 3: Start REST server
make rest-server

# Terminal 4: Test endpoints
curl http://localhost:8080/health
curl http://localhost:8080/api/v1/accounts/0.0.100000/balance
grpcurl -plaintext localhost:50051 list
```

---

## Next Step: Benchmark Runner

After code review, implement `cmd/benchmark/main.go`:

```bash
# Target CLI interface
./benchmark --scenario=balance --protocol=grpc --concurrency=50 --duration=60s
./benchmark --scenario=stream --protocol=rest --rate=100 --duration=60s
```

**Key components:**
- CLI flag parsing (scenario, protocol, concurrency, duration, rate)
- gRPC client with connection reuse
- HTTP client with keep-alive
- Concurrent worker pool for load generation
- Latency measurement (time.Now → response received)
- Results storage via `pkg/db/benchmark.go`
- Console summary output

---

## What's Done

- `migrations/001_init.sql` - Full schema with accounts, transactions, benchmark results, stats view
- `pkg/protos/benchmark.proto` - BalanceService, TransactionService, Health definitions
- `pkg/protos/benchmark.pb.go` - Generated message types
- `pkg/protos/benchmark_grpc.pb.go` - Generated service interfaces
- `scripts/seed_data.sql` - 10K accounts, 100K transactions with realistic distribution
- `docker-compose.yml` - PostgreSQL 16 with health checks
- `Makefile` - proto, seed, benchmark, grpc-server, rest-server, clean targets
- `pkg/db/*.go` - Database client layer
- `cmd/grpc-server/main.go` - gRPC server implementation
- `cmd/rest-server/main.go` - REST server implementation

---

## Scenario 1: Account Balance Query

**Business Context:** Financial systems query balances for dashboards, portfolios, reconciliation.

**Endpoints:**
| Protocol | Endpoint | Payload |
|----------|----------|---------|
| REST | `GET /api/v1/accounts/{id}/balance` | ~100 bytes JSON |
| gRPC | `BalanceService.GetBalance()` | ~50 bytes protobuf |

**Workload:**
- Random account selection (uniform distribution)
- No caching (force DB hit)
- Concurrency levels: 1, 10, 50, 100, 200

**Success Criteria:**
- p99 latency < 100ms at 50 concurrent
- No errors under 100 concurrent requests

**Metrics:** Latency (p50/p90/p99), throughput (req/s), error rate

---

## Scenario 2: Transaction Event Stream

**Business Context:** Real-time monitoring for block explorers, trading dashboards, compliance.

**Endpoints:**
| Protocol | Endpoint | Implementation |
|----------|----------|----------------|
| REST | `GET /api/v1/transactions/stream` | Server-Sent Events (SSE) |
| gRPC | `TransactionService.StreamTransactions()` | Server streaming RPC |

**Workload:**
| Scenario | Rate (tx/sec) | Duration | Clients |
|----------|---------------|----------|---------|
| Low load | 10 | 60s | 1 |
| Medium load | 100 | 60s | 5 |
| High load | 500 | 60s | 10 |
| Burst | 1000 | 10s | 1 |

**Success Criteria:**
- No dropped events
- Events arrive in order
- Connection stable for full duration

**Metrics:** Event delivery latency, throughput, connection overhead, missed events

---

## Implementation Tasks

### Step 1: Generate Proto & Add Dependencies
```bash
make proto
```

Update `go.mod`:
```
google.golang.org/grpc
google.golang.org/protobuf
github.com/jackc/pgx/v5
```

### Step 2: Database Client (`pkg/db/`)

| File | Functions |
|------|-----------|
| `db.go` | `NewPool(connString)`, connection config |
| `accounts.go` | `GetBalance(accountID)`, `GetBalances([]accountID)` |
| `transactions.go` | `StreamTransactions(since, rateLimit, filter)` |
| `benchmark.go` | `RecordRun()`, `RecordSample()` |

### Step 3: gRPC Server (`cmd/grpc-server/`)

| File | Purpose |
|------|---------|
| `main.go` | Bootstrap, listen `:50051` |
| `balance.go` | Implement `GetBalance`, `GetBalances` |
| `transactions.go` | Implement `StreamTransactions` |
| `health.go` | Implement health check |

### Step 4: REST Server (`cmd/rest-server/`)

| Endpoint | Handler |
|----------|---------|
| `GET /api/v1/accounts/{id}/balance` | JSON response |
| `GET /api/v1/accounts/balances?ids=...` | Batch JSON |
| `GET /api/v1/transactions/stream` | SSE with `text/event-stream` |
| `GET /health` | Health check |

Use stdlib `net/http` to keep comparison fair (no framework overhead).

### Step 5: Benchmark Runner (`cmd/benchmark/`)

| File | Purpose |
|------|---------|
| `main.go` | CLI flags, orchestration |
| `grpc_client.go` | gRPC client with connection pooling |
| `rest_client.go` | HTTP client with keep-alive |
| `runner.go` | Concurrent load generation |
| `reporter.go` | Write to DB, console output |

**CLI Usage:**
```bash
# Balance query benchmark
./benchmark --scenario=balance --protocol=grpc --concurrency=50 --duration=60s

# Streaming benchmark
./benchmark --scenario=stream --protocol=rest --rate=100 --duration=60s
```

---

## Target File Structure

```
grpc-rest-benchmark/
├── cmd/
│   ├── grpc-server/
│   │   └── main.go
│   ├── rest-server/
│   │   └── main.go
│   └── benchmark/
│       └── main.go
├── pkg/
│   ├── db/
│   │   ├── db.go
│   │   ├── accounts.go
│   │   ├── transactions.go
│   │   └── benchmark.go
│   └── protos/
│       ├── benchmark.proto          (exists)
│       ├── benchmark.pb.go          (generated)
│       └── benchmark_grpc.pb.go     (generated)
├── migrations/
│   └── 001_init.sql                 (exists)
├── scripts/
│   └── seed_data.sql                (exists)
├── docker-compose.yml               (exists)
├── Makefile                         (exists)
└── go.mod                           (exists, needs deps)
```

---

## Dependency Order

```
Step 1 ──► Step 2 ──► Step 3 ──► Step 5
                 └──► Step 4 ──┘
```

Steps 3 and 4 (servers) can be built in parallel after Step 2 (db client).

---

## Expected Results

**Hypothesis to test:**
- gRPC lower latency on balance queries (binary encoding, HTTP/2)
- gRPC lower per-event overhead on streaming
- REST SSE simpler connection handling but higher payload size
- gRPC better backpressure handling (built-in flow control)

**Payload comparison:**
| Scenario | REST JSON | gRPC Protobuf |
|----------|-----------|---------------|
| Balance response | ~100 bytes | ~50 bytes |
| Transaction event | ~200 bytes | ~100 bytes |
