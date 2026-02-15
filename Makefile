.PHONY: proto seed benchmark clean db-up db-down

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

# Run benchmarks
benchmark:
	go run cmd/benchmark/main.go

# Clean up: stop containers, remove volumes, delete generated code
clean:
	docker-compose down -v
	rm -f pkg/protos/*.pb.go
