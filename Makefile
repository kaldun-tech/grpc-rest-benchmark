.PHONY: proto seed benchmark clean

proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       pkg/protos/benchmark.proto

seed:
	docker exec -i grpc-benchmark_postgres_1 psql -U benchmark -d grpc_benchmark < scripts/seed_data.sql

benchmark:
	go run cmd/benchmark/main.go

clean:
	docker-compose down -v
	rm -f pkg/protos/*.pb.go
