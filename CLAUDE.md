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

# Run benchmarks
make benchmark

# Clean up (stop containers, remove generated protobuf code)
make clean
```

## Prerequisites

- Go (with modules)
- Protocol Buffer compiler (`protoc`) with Go plugins (`protoc-gen-go`, `protoc-gen-go-grpc`)
- Docker and Docker Compose (for PostgreSQL)

## Architecture

```
cmd/
  grpc-server/main.go    # gRPC server on :50051
  rest-server/main.go    # REST server on :8080
  benchmark/main.go      # CLI benchmark runner
pkg/
  db/                    # PostgreSQL client (accounts, transactions, benchmark results)
  protos/                # Proto definitions and generated Go code
migrations/001_init.sql  # Schema: accounts, transactions, benchmark tables
scripts/seed_data.sql    # 10K accounts, 100K transactions
docker-compose.yml       # PostgreSQL 16
```

**Benchmark scenarios:**
1. Balance queries — high-frequency unary requests
2. Transaction streaming — server-side streaming (gRPC) vs SSE (REST)
