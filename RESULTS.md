# Benchmark Results

## Test Environment

- **Platform:** WSL2 (Linux 6.6.87)
- **Database:** PostgreSQL 16 (Docker)
- **Data:** 10K accounts, 100K transactions
- **Duration:** 10 seconds per test

---

## Scenario 1: Balance Query

Single account balance lookup hitting PostgreSQL.

### Results by Concurrency

| Concurrency | gRPC req/s | REST req/s | gRPC p99 | REST p99 | Winner |
|-------------|------------|------------|----------|----------|--------|
| 10 | 6,496 | 6,806 | 3.13ms | 4.36ms | REST throughput, gRPC latency |
| 50 | 10,652 | 9,076 | 13.17ms | 15.43ms | **gRPC +17%** |
| 100 | 8,912 | 7,546 | 31.24ms | 40.05ms | **gRPC +18%** |

### Key Observations

1. **Low concurrency (10):** REST has ~5% higher throughput due to lower protocol overhead for simple requests
2. **High concurrency (50+):** gRPC pulls ahead by 17-18% due to HTTP/2 multiplexing
3. **Tail latency:** gRPC consistently has 15-22% better p99 under load
4. **Throughput ceiling:** Both hit peak around 50 concurrent workers (DB connection saturation)

---

## Scenario 2: Transaction Streaming

Server-push of transaction events (gRPC server streaming vs REST SSE).

### Results by Rate

| Target Rate | Clients | gRPC events/s | REST events/s | gRPC p99 | REST p99 |
|-------------|---------|---------------|---------------|----------|----------|
| 100/s | 1 | 100 | 100 | 11.03ms | 11.28ms |
| 500/s | 5 | 2,485 | 2,465 | 3.18ms | 3.50ms |
| 1000/s | 10 | 9,055 | 9,023 | 2.19ms | 2.05ms |

### Key Observations

1. **Both protocols deliver:** ~9K events/sec with zero dropped events
2. **Latency parity:** Inter-event timing nearly identical
3. **Connection stability:** Zero errors across all streaming tests
4. **No clear winner:** For streaming, choose based on ecosystem fit, not performance

---

## Summary

| Use Case | Recommendation |
|----------|----------------|
| High-frequency queries under load | **gRPC** (+17% throughput, +22% better p99) |
| Simple queries, low concurrency | REST (slightly simpler, marginally faster) |
| Event streaming | Either (both handle 9K events/sec cleanly) |
| Browser clients required | REST/SSE (native browser support) |
| Polyglot microservices | gRPC (schema-first, code generation) |

---

## Methodology Notes

- Latency measured client-side (request sent → response received)
- Streaming latency = inter-event timing (time between consecutive events)
- Errors at benchmark end are context cancellation, not failures
- Results stored in PostgreSQL for reproducibility (`benchmark_runs`, `benchmark_samples` tables)
