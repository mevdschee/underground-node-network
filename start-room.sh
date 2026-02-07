#!/bin/bash
set -e

cd "$(dirname "$0")"

# Build
echo "Building unn-room..."
go build -o unn-room-bin ./cmd/unn-room

# Start with random port (0 = OS assigns)
echo "Starting UNN room..."
./unn-room-bin -entrypoint www.wink-sys.nl:44322 -bind 0.0.0.0 -port 0 -room "myroom"
