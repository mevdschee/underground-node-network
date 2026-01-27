#!/bin/bash
set -e

cd "$(dirname "$0")"

# Build
echo "Building unn-intro..."
go build -o unn-intro-bin ./cmd/unn-intro

# Execute
echo "Starting UNN intro..."
./unn-intro-bin "$@"
