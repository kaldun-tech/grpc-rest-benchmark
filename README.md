# gRPC vs REST Performance Benchmark

Performance comparison of gRPC and REST protocols for financial infrastructure workloads commonly found in blockchain custody, exchanges, and payment systems.

Built in Go with a PostgreSQL backend to isolate transport layer performance from application logic.

## Quick Start

```bash
# Prerequisites: Go 1.21+, Docker, protoc with Go plugins

# Generate protobuf code
make proto

# Start PostgreSQL and seed test data
make seed

# Run benchmarks
make benchmark

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

## Project Structure

```
├── cmd/
│   ├── server/          # gRPC + REST server
│   └── benchmark/       # Benchmark runner
├── pkg/
│   ├── protos/          # Protocol buffer definitions
│   ├── db/              # PostgreSQL client
│   └── models/          # Shared data types
├── migrations/          # Database schema
└── scripts/             # Seed data generation
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
