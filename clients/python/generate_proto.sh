#!/bin/bash
# Generate Python protobuf stubs from benchmark.proto

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROTO_DIR="$SCRIPT_DIR/../../pkg/protos"
OUT_DIR="$SCRIPT_DIR/proto"

mkdir -p "$OUT_DIR"

python -m grpc_tools.protoc \
    -I "$PROTO_DIR" \
    --python_out="$OUT_DIR" \
    --grpc_python_out="$OUT_DIR" \
    "$PROTO_DIR/benchmark.proto"

# Fix imports in generated grpc file to use relative imports
sed -i 's/^import benchmark_pb2/from . import benchmark_pb2/' "$OUT_DIR/benchmark_pb2_grpc.py"

# Create __init__.py for the proto package
touch "$OUT_DIR/__init__.py"

echo "Generated Python proto stubs in $OUT_DIR"
