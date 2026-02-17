.PHONY: proto seed benchmark clean db-up db-down grpc-server rest-server servers \
        benchmark-balance-grpc benchmark-balance-rest benchmark-stream-grpc benchmark-stream-rest

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

# Clean up: stop containers, remove volumes, delete generated code
clean:
	docker-compose down -v
	rm -f pkg/protos/*.pb.go
