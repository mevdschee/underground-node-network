#!/bin/bash
set -e

cd "$(dirname "$0")"

# Build
echo "Building unn-client..."
go build -o unn-client-bin ./cmd/unn-client

# Start with random port (0 = OS assigns)
echo "Starting UNN client..."
./unn-client-bin -entrypoint localhost:44322 -bind 0.0.0.0 -port 0 -room "myroom" -user maurits
