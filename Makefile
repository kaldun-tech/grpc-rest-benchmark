.PHONY: proto seed benchmark clean db-up db-down grpc-server rest-server servers

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

# Run benchmarks
benchmark:
	go run cmd/benchmark/main.go

# Clean up: stop containers, remove volumes, delete generated code
clean:
	docker-compose down -v
	rm -f pkg/protos/*.pb.go
