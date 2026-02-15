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

**Planned structure:**
- `cmd/benchmark/main.go` - Main benchmark entry point
- `pkg/protos/benchmark.proto` - Protocol buffer definitions
- `scripts/seed_data.sql` - PostgreSQL seed data
- `docker-compose.yml` - PostgreSQL container setup

**Test scenarios:**
1. High-frequency balance queries
2. Real-time transaction event streaming
3. Bulk historical data retrieval
4. State update operations
