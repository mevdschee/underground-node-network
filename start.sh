#!/bin/bash
set -e

cd "$(dirname "$0")"

# Build
echo "Building unn-client..."
go build -o unn-client ./cmd/unn-client

# Start
echo "Starting UNN client..."
./unn-client "$@"
