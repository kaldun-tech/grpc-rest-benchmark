.PHONY: proto seed benchmark clean db-up db-down grpc-server rest-server servers \
        benchmark-balance-grpc benchmark-balance-rest benchmark-stream-grpc benchmark-stream-rest \
        python-deps python-proto python-benchmark python-sdk-benchmark migrate \
        fetch-hcs-timing benchmark-replay \
        rust-build rust-benchmark \
        test test-db test-benchmark \
        dashboard-check api-check

# Generate Go code from Protocol Buffers
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       pkg/protos/benchmark.proto

# Start PostgreSQL container
db-up:
	docker-compose up -d postgres
	@echo "Waiting for PostgreSQL to be ready..."
	@until docker-compose exec -T postgres pg_isready -U benchmark > /dev/null 2>&1; do sleep 1; done
	@echo "PostgreSQL is ready"

# Stop PostgreSQL container
db-down:
	docker-compose down

# Seed database with test data (10k accounts, 100k transactions)
seed: db-up
	docker-compose exec -T postgres psql -U benchmark -d grpc_benchmark < scripts/seed_data.sql

# Run gRPC server (port 50051)
grpc-server: db-up
	go run cmd/grpc-server/main.go

# Run REST server (port 8080)
rest-server: db-up
	go run cmd/rest-server/main.go

# Run both servers (in background) - use 'make db-down' to stop
servers: db-up
	@echo "Starting gRPC server on :50051..."
	@go run cmd/grpc-server/main.go &
	@echo "Starting REST server on :8080..."
	@go run cmd/rest-server/main.go &
	@echo "Both servers running. Use 'pkill -f cmd/..-server' to stop."

# Run benchmarks (use ARGS to pass flags, e.g.: make benchmark ARGS="--scenario=balance --protocol=grpc")
benchmark:
	go run cmd/benchmark/*.go $(ARGS)

# Quick benchmark examples
benchmark-balance-grpc:
	go run cmd/benchmark/*.go --scenario=balance --protocol=grpc --duration=10s --concurrency=10

benchmark-balance-rest:
	go run cmd/benchmark/*.go --scenario=balance --protocol=rest --duration=10s --concurrency=10

benchmark-stream-grpc:
	go run cmd/benchmark/*.go --scenario=stream --protocol=grpc --duration=10s --rate=100

benchmark-stream-rest:
	go run cmd/benchmark/*.go --scenario=stream --protocol=rest --duration=10s --rate=100

# Run database migrations
migrate: db-up
	@for f in migrations/*.sql; do \
		echo "Running migration: $$f"; \
		docker-compose exec -T postgres psql -U benchmark -d grpc_benchmark < "$$f"; \
	done

# Python client targets
python-deps:
	pip install -r clients/python/requirements.txt

python-proto: python-deps
	cd clients/python && ./generate_proto.sh

python-benchmark: python-proto
	python clients/python/grpc_client.py $(ARGS)

# Python SDK benchmark (requires HEDERA_OPERATOR_ID and HEDERA_OPERATOR_KEY env vars)
python-sdk-benchmark: python-deps
	python clients/python/sdk_client.py $(ARGS)

# HCS timing fetch (Phase 2d) - fetch timing distribution from Hedera topic
# Usage: make fetch-hcs-timing TOPIC=0.0.120438 NETWORK=mainnet LIMIT=1000
TOPIC ?= 0.0.120438
NETWORK ?= mainnet
LIMIT ?= 1000
fetch-hcs-timing:
	python3 scripts/fetch_hcs_timing.py --topic=$(TOPIC) --network=$(NETWORK) --limit=$(LIMIT) --output=timing.json

# Run benchmark with HCS timing replay (Phase 2d)
# Usage: make benchmark-replay TIMING=timing.json ARGS="--protocol=grpc --concurrency=10"
TIMING ?= timing.json
SPEEDUP ?= 1.0
benchmark-replay:
	go run cmd/benchmark/*.go --scenario=balance --replay-timing=$(TIMING) --replay-speedup=$(SPEEDUP) $(ARGS)

# Rust client targets (Phase 2e)
rust-build:
	cd clients/rust && cargo build --release

rust-benchmark: rust-build
	./clients/rust/target/release/benchmark_client $(ARGS)

# Test targets (Phase 2f)
test: test-db test-benchmark
	@echo "All tests passed!"

test-db: db-up
	go test ./pkg/db/... -v -count=1

test-benchmark:
	go test ./cmd/benchmark/... -v -count=1

# Dashboard check (Phase 3)
dashboard-check:
	@curl -s http://localhost:8080/ | grep -q "gRPC vs REST" && echo "Dashboard OK" || echo "Dashboard not running"

# Results API check (Phase 3)
api-check:
	@curl -s http://localhost:8080/api/v1/results | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'Results API OK: {d[\"count\"]} results')"

# Clean up: stop containers, remove volumes, delete generated code
clean:
	docker-compose down -v
	rm -f pkg/protos/*.pb.go
	rm -rf clients/python/proto
	rm -rf clients/rust/target
	rm -f timing.json
