# When Should You Use gRPC? Here's the Data

*A practical performance comparison for financial infrastructure workloads*

---

## Introduction

The gRPC vs REST debate often devolves into tribal preferences. "gRPC is faster!" vs "REST is simpler!" Neither camp provides much data.

We built a benchmark to answer concrete questions: When does gRPC actually outperform REST? By how much? And when does it not matter?

Our test workloads mirror real financial infrastructure: account balance queries (think wallet apps, pre-transaction validation) and transaction event streams (audit trails, real-time dashboards). We isolated the transport layer by using identical Go servers with PostgreSQL backends.

Here's what we found.

---

## Methodology

### Test Setup

- **Platform:** WSL2 (Linux 6.6.87)
- **Database:** PostgreSQL 16 (Docker)
- **Data:** 10,000 accounts, 100,000 transactions
- **Duration:** 10 seconds per test
- **Clients:** Go (baseline), Python (grpcio), Rust (tonic)

### Scenarios

**Scenario 1: Balance Queries**
- gRPC: `BalanceService.GetBalance(account_id) → BalanceResponse`
- REST: `GET /accounts/{id}/balance → JSON`
- Payload: ~50 bytes (protobuf) vs ~100 bytes (JSON)

**Scenario 2: Transaction Streaming**
- gRPC: Server streaming RPC
- REST: Server-Sent Events (SSE)
- Pattern: Continuous event push, variable rates

---

## Results

### High-Frequency Queries: gRPC Wins Under Load

| Concurrency | gRPC req/s | REST req/s | gRPC p99 | REST p99 | Difference |
|-------------|------------|------------|----------|----------|------------|
| 10 | 6,496 | 6,806 | 3.13ms | 4.36ms | REST +5% throughput |
| 50 | 10,652 | 9,076 | 13.17ms | 15.43ms | **gRPC +17%** |
| 100 | 8,912 | 7,546 | 31.24ms | 40.05ms | **gRPC +18%** |

At low concurrency, REST has marginally higher throughput. The HTTP/1.1 connection overhead is negligible for simple requests.

But at 50+ concurrent workers, gRPC pulls ahead significantly:
- **17-18% higher throughput** due to HTTP/2 multiplexing
- **15-22% lower p99 latency** under sustained load

Both protocols hit database connection pool saturation around 10K req/s — the transport layer isn't the bottleneck at that point.

### Streaming: It's a Wash

| Target Rate | gRPC events/s | REST events/s | gRPC p99 | REST p99 |
|-------------|---------------|---------------|----------|----------|
| 100/s | 100 | 100 | 11.03ms | 11.28ms |
| 500/s | 2,485 | 2,465 | 3.18ms | 3.50ms |
| 1000/s | 9,055 | 9,023 | 2.19ms | 2.05ms |

Both gRPC streaming and REST SSE deliver ~9,000 events/second with:
- Zero dropped events
- Near-identical latency
- No connection errors

For event streaming, choose based on ecosystem fit, not performance.

---

## When to Use What

| Use Case | Recommendation | Why |
|----------|----------------|-----|
| High-frequency queries under load | **gRPC** | +17% throughput, +22% better p99 |
| Simple queries, low concurrency | REST | Slightly simpler, marginally faster |
| Event streaming | Either | Both handle 9K events/sec cleanly |
| Browser clients | REST/SSE | Native browser support |
| Polyglot microservices | gRPC | Schema-first, code generation |
| Public APIs | REST | Better tooling, discoverability |

---

## Multi-Language Overhead

We also tested Python and Rust clients against the same Go servers:

| Client | Protocol | Throughput | p99 Latency | Notes |
|--------|----------|------------|-------------|-------|
| Go | gRPC | 10,652/s | 13.17ms | Baseline |
| Rust (tonic) | gRPC | ~10,500/s | ~13ms | Near parity |
| Python (grpcio) | gRPC | ~3,000/s | ~35ms | GIL overhead |

Rust performs nearly identically to Go. Python's GIL limits throughput but is adequate for many use cases.

---

## What We Learned

1. **gRPC's advantage is real, but conditional.** You need sustained concurrent load (50+ workers) to see meaningful benefits.

2. **For streaming, choose based on ecosystem.** If you need browser support, use SSE. If you're already in a gRPC ecosystem, use gRPC streaming.

3. **The database is usually the bottleneck.** Both protocols maxed out around 10K req/s — constrained by PostgreSQL connection pool, not transport.

4. **Language matters more than protocol.** A well-tuned Python REST client will outperform a poorly configured Go gRPC client.

---

## Try It Yourself

The benchmark is open source. Run your own tests:

```bash
git clone https://github.com/your-org/grpc-rest-benchmark
cd grpc-rest-benchmark
make seed
make rest-server &
make grpc-server &
make benchmark ARGS="--scenario=balance --protocol=grpc --concurrency=50"
```

Dashboard available at http://localhost:8080/ when the REST server is running.

---

## Conclusion

gRPC delivers measurable performance benefits under load: 17% higher throughput and 22% better tail latency for high-frequency queries. But it's not a silver bullet.

For simple APIs with moderate traffic, REST remains a pragmatic choice. For high-throughput internal services, gRPC's advantages compound.

The best protocol is the one your team can operate reliably. Performance differences matter less than observability, debugging tools, and deployment simplicity.

Choose based on your constraints, not benchmarks. But when performance matters, now you have the data.
