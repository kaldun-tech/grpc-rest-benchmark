# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Performance benchmark comparing gRPC and REST protocols for financial infrastructure workloads (blockchain custody, exchanges, payment systems). Built in Go with PostgreSQL backend to isolate transport layer performance.

## Build Commands

```bash
# Compile Protocol Buffers to Go code
make proto

# Seed PostgreSQL database with test data
make seed

# Start servers
make grpc-server   # :50051
make rest-server   # :8080

# Run benchmarks (Go, Python, Rust)
make go-benchmark ARGS="--scenario=balance --protocol=grpc --duration=10s"
make python-benchmark ARGS="--scenario=balance --duration=10s"
make rust-benchmark ARGS="--scenario=balance --protocol=grpc --duration=10s"

# Run tests
make test

# Clean up (stop containers, remove generated protobuf code, venvs)
make clean
```

## Prerequisites

- Go 1.21+ (with modules)
- Protocol Buffer compiler (`protoc`) with Go plugins (`protoc-gen-go`, `protoc-gen-go-grpc`)
- Docker and Docker Compose (for PostgreSQL)
- Python 3.12+ (optional, for Python benchmarks - venv auto-created)
- Rust/Cargo (optional, for Rust benchmarks)

## Architecture

```
cmd/
  grpc-server/main.go    # gRPC server on :50051
  rest-server/main.go    # REST server on :8080
  benchmark/main.go      # Go benchmark client (CLI runner)
pkg/
  db/                    # PostgreSQL client (accounts, transactions, benchmark results)
  protos/                # Proto definitions and generated Go code
clients/
  python/                # Python gRPC + SDK benchmark clients
  rust/                  # Rust benchmark client (tonic + reqwest)
web/                     # Dashboard (index.html, dashboard.js, style.css)
migrations/              # Database schema (001-004)
scripts/seed_data.sql    # 10K accounts, 100K transactions
docker-compose.yml       # PostgreSQL 16
```

**Benchmark scenarios:**
1. Balance queries — high-frequency unary requests
2. Transaction streaming — server-side streaming (gRPC) vs SSE (REST)
