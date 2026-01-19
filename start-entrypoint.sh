#!/bin/bash
set -e

cd "$(dirname "$0")"

# Build
echo "Building unn-entrypoint..."
go build -o unn-entrypoint-bin ./cmd/unn-entrypoint

# Start
echo "Starting UNN Entry Point..."
./unn-entrypoint-bin "$@"
