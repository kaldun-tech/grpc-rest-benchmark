# gRPC vs REST Performance Benchmark

Performance comparison of gRPC and REST for financial infrastructure workloads 
commonly found in blockchain custody, exchanges, and payment systems.

Scenarios tested:
- High-frequency balance queries
- Real-time transaction event streaming
- Bulk historical data retrieval
- State update operations

Built in Go with PostgreSQL backend to isolate transport layer performance.
